package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/repository"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// Start errors. StartDelivery returns these typed sentinels so a transport
// (API/CLI) can map each to the right status code without parsing strings.
var (
	// ErrStartDuplicate is returned when an execution for the same
	// deterministic workflow ID already exists (a double-click, HTTP retry or
	// Telegram retry collapses to one execution).
	ErrStartDuplicate = errors.New("kernel: delivery already started for this plan+attempt")
	// ErrStartRefused is returned when the plan is revoked/expired, the lane
	// is unknown, or the executor allowlist is empty (fail-closed).
	ErrStartRefused = errors.New("kernel: delivery refused")
)

// WorkflowStarter is the subset of go.temporal.io/sdk/client.Client that
// StartDelivery needs. It is an interface so StartDelivery is unit-testable
// without a live Temporal server. *client.Client satisfies it.
type WorkflowStarter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
}

// StartDeliveryInput is the transport-supplied part of a delivery start. The
// transport may name the plan, the resolved plan source, the repo working
// path and (optionally) a lane and attempt ordinal — it may NOT name an
// executor, a task queue or a workflow ID: those are kernel-resolved
// (Constitution C4). Task 141 adds ownership/intent references only; authority
// fields are resolved into an ExecutionEnvelope.
type StartDeliveryInput struct {
	PlanID       string
	PlanFilePath string
	RepoPath     string
	Lane         string
	Attempt      int

	// Intent / ownership references (Task 141). Transports may set these;
	// they never carry executor/policy/budget/sandbox authority.
	MissionID                string
	PortfolioID              string
	ProfileID                string
	OrganizationID           string
	PrincipalID              string
	Unattended               bool
	RepositoryID             string
	Provider                 string
	CanonicalURL             string
	RepositoryAlias          string
	PinnedBaseRevision       string
	TargetBranch             string
	PlanArtifactRef          string
	BudgetEnvelopeID         string
	SessionCapUSD            float64
	ExperimentCapUSD         float64
	DeploymentCapUSD         float64
	MaxWaveConcurrency       int
	BranchDeliveryPolicy     string
	PermittedEffects         []string
	AuthorizationDecisionRef string
}

// StartDeliveryOutput reports what the kernel resolved and started.
type StartDeliveryOutput struct {
	WorkflowID     string
	RunID          string
	TaskQueue      string
	Lane           string
	EnvelopeID     string
	EnvelopeDigest string
}

// StartDeps bundles the kernel-owned resolution inputs StartDelivery needs.
type StartDeps struct {
	Starter           WorkflowStarter
	Provenance        *provenance.Store
	QueueConfig       observe.QueueConfig
	LaneSelector      LaneSelector
	ExecutorAllowlist []string
	// Transitions, when set, records an initial started transition so
	// `foundry status` observes the execution immediately.
	Transitions TransitionStore
	// EnvelopeStore + Policy resolve and persist the Task 141 execution
	// envelope before Temporal start. Unattended starts refuse when either
	// is missing (C24).
	EnvelopeStore EnvelopeStore
	Policy        *compiler.Resolved
	LayerDigests  []string
	PolicyVersion string
	Now           func() time.Time
	// RepositoryStore resolves owned repository IDs (Task 143) when
	// StartDeliveryInput.RepositoryID is set.
	RepositoryStore   repository.Store
	AllowedLocalRoots []string
}

// DeliveryWorkflowID derives the deterministic workflow ID from the plan
// digest, attempt ordinal, and (when present) execution-envelope digest, so a
// double-click, a retried HTTP request and a Telegram retry all collapse to
// one execution rather than three (docs/PLAN.md Task 141 binds the envelope
// digest into the idempotency key).
func DeliveryWorkflowID(planDigest string, attempt int, envelopeDigest string) string {
	payload := fmt.Sprintf("%s|%d", planDigest, attempt)
	if envelopeDigest != "" {
		payload = fmt.Sprintf("%s|%s", payload, envelopeDigest)
	}
	sum := sha256.Sum256([]byte(payload))
	return "deliver-" + hex.EncodeToString(sum[:])[:32]
}

// StartDelivery is Foundry's single production edge from an ApprovedPlan to a
// running DeliverPlan execution (docs/PLAN.md Task 105 / RTC-01, Task 141 /
// RTC-05). The kernel — never the transport — resolves the lane, the executor
// allowlist, the execution envelope and the workflow ID (Constitution C4).
func StartDelivery(ctx context.Context, deps StartDeps, in StartDeliveryInput) (StartDeliveryOutput, error) {
	if deps.Starter == nil {
		return StartDeliveryOutput{}, fmt.Errorf("kernel: StartDelivery requires a workflow starter")
	}
	if deps.Provenance == nil {
		return StartDeliveryOutput{}, fmt.Errorf("kernel: StartDelivery requires a provenance store")
	}
	if in.PlanID == "" {
		return StartDeliveryOutput{}, fmt.Errorf("kernel: StartDelivery requires a plan id")
	}

	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}

	// (1) Load the ApprovedPlan through provenance.Store.Load so revocation
	// and expiry are enforced at start, not only at wave boundaries.
	approved, err := deps.Provenance.Load(ctx, in.PlanID)
	if err != nil {
		if errors.Is(err, provenance.ErrPlanRevoked) || errors.Is(err, provenance.ErrPlanExpired) {
			return StartDeliveryOutput{}, fmt.Errorf("%w: plan %s is not open: %v", ErrStartRefused, in.PlanID, err)
		}
		return StartDeliveryOutput{}, fmt.Errorf("kernel: load approved plan %s: %w", in.PlanID, err)
	}

	// (2) Resolve the lane's task queue (fail-closed on an unknown lane).
	queue, err := deps.LaneSelector.Select(in.Lane, deps.QueueConfig)
	if err != nil {
		return StartDeliveryOutput{}, fmt.Errorf("%w: %v", ErrStartRefused, err)
	}

	// (3) Resolve the execution envelope when policy+store are wired, or when
	// the start is unattended (mandatory). Unattended without wiring refuses.
	var env *ExecutionEnvelope
	executorAllowlist := append([]string(nil), deps.ExecutorAllowlist...)
	maxWave := in.MaxWaveConcurrency
	requireSandbox := false
	unattended := in.Unattended
	missionID := in.MissionID

	if in.Unattended && (deps.EnvelopeStore == nil || deps.Policy == nil) {
		return StartDeliveryOutput{}, fmt.Errorf("%w: unattended execution without an envelope", ErrStartRefused)
	}

	// Task 143: when a repository ID is named, resolve it from the owned
	// registry before envelope creation — never trust a transport path as
	// authority for production unattended starts.
	if in.RepositoryID != "" && deps.RepositoryStore != nil {
		resolvedRepo, resolveErr := (repository.Resolver{Store: deps.RepositoryStore}).Resolve(ctx, repository.ResolveInput{
			RepositoryID:      in.RepositoryID,
			ProfileID:         in.ProfileID,
			OrganizationID:    in.OrganizationID,
			AllowedLocalRoots: deps.AllowedLocalRoots,
			RequirePinned:     in.Unattended,
		})
		if resolveErr != nil {
			return StartDeliveryOutput{}, fmt.Errorf("%w: repository: %v", ErrStartRefused, resolveErr)
		}
		in.Provider = resolvedRepo.Record.Provider
		in.CanonicalURL = resolvedRepo.Record.CanonicalURL
		in.RepositoryAlias = resolvedRepo.Record.Alias
		if in.PinnedBaseRevision == "" {
			in.PinnedBaseRevision = resolvedRepo.Record.PinnedBaseRevision
		}
		if in.TargetBranch == "" {
			in.TargetBranch = resolvedRepo.Record.DefaultTargetBranch
		}
	}

	if deps.Policy != nil && deps.EnvelopeStore != nil {
		resolved, resolveErr := ResolveExecutionEnvelope(ctx, EnvelopeResolverDeps{
			Provenance:    deps.Provenance,
			Policy:        deps.Policy,
			LayerDigests:  deps.LayerDigests,
			PolicyVersion: deps.PolicyVersion,
			Now:           func() time.Time { return now },
		}, ResolveExecutionEnvelopeInput{
			PlanID:                   in.PlanID,
			PlanArtifactRef:          in.PlanArtifactRef,
			RepositoryID:             in.RepositoryID,
			Provider:                 in.Provider,
			CanonicalURL:             in.CanonicalURL,
			RepositoryAlias:          in.RepositoryAlias,
			PinnedBaseRevision:       in.PinnedBaseRevision,
			TargetBranch:             in.TargetBranch,
			MissionID:                in.MissionID,
			PortfolioID:              in.PortfolioID,
			ProfileID:                in.ProfileID,
			OrganizationID:           in.OrganizationID,
			PrincipalID:              in.PrincipalID,
			Unattended:               in.Unattended,
			MaxWaveConcurrency:       in.MaxWaveConcurrency,
			BudgetEnvelopeID:         in.BudgetEnvelopeID,
			SessionCapUSD:            in.SessionCapUSD,
			ExperimentCapUSD:         in.ExperimentCapUSD,
			DeploymentCapUSD:         in.DeploymentCapUSD,
			BranchDeliveryPolicy:     in.BranchDeliveryPolicy,
			PermittedEffects:         in.PermittedEffects,
			AuthorizationDecisionRef: in.AuthorizationDecisionRef,
			IssuedAt:                 now,
		})
		if resolveErr != nil {
			return StartDeliveryOutput{}, fmt.Errorf("%w: %v", ErrStartRefused, resolveErr)
		}
		if err := deps.EnvelopeStore.Insert(ctx, resolved); err != nil {
			return StartDeliveryOutput{}, fmt.Errorf("kernel: persist execution envelope: %w", err)
		}
		env = resolved
		executorAllowlist = append([]string(nil), resolved.Execution.ExecutorAllowlist...)
		maxWave = resolved.Execution.MaxWaveConcurrency
		requireSandbox = resolved.Execution.RequireSandbox
		unattended = resolved.Execution.Unattended
		missionID = resolved.Ownership.MissionID
	}

	// (4) The executor allowlist must be non-nil and non-empty. A nil/empty
	// allowlist is the fail-open path Task 116 closes — refuse rather than run
	// with no policy (Constitution C4).
	if len(executorAllowlist) == 0 {
		return StartDeliveryOutput{}, fmt.Errorf("%w: empty executor allowlist", ErrStartRefused)
	}

	envelopeDigest := ""
	envelopeID := ""
	if env != nil {
		envelopeDigest = env.EnvelopeDigest
		envelopeID = env.EnvelopeID
	}

	// (5) Deterministic, idempotent workflow ID (envelope-bound when present).
	workflowID := DeliveryWorkflowID(approved.PlanDigest(), in.Attempt, envelopeDigest)

	run, err := deps.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             queue,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, DeliverPlan, DeliverPlanInput{
		PlanID:             in.PlanID,
		PlanFilePath:       in.PlanFilePath,
		RepoPath:           in.RepoPath,
		ExecutorAllowlist:  executorAllowlist,
		MaxWaveConcurrency: maxWave,
		EnvelopeID:         envelopeID,
		EnvelopeDigest:     envelopeDigest,
		MissionID:          missionID,
		Unattended:         unattended,
		RequireSandbox:     requireSandbox,
		BudgetScope:        envelopeBudgetScope(env),
		BudgetScopeID:      envelopeBudgetScopeID(env),
	})
	if err != nil {
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &already) {
			return StartDeliveryOutput{}, fmt.Errorf("%w: workflow %s", ErrStartDuplicate, workflowID)
		}
		return StartDeliveryOutput{}, fmt.Errorf("kernel: start workflow %s: %w", workflowID, err)
	}

	out := StartDeliveryOutput{
		WorkflowID:     workflowID,
		RunID:          run.GetRunID(),
		TaskQueue:      queue,
		Lane:           laneOrDefault(in.Lane),
		EnvelopeID:     envelopeID,
		EnvelopeDigest: envelopeDigest,
	}

	// (6) Record an initial transition so `foundry status` sees the execution
	// immediately (best-effort: a transition-store failure does not undo a
	// started workflow).
	if deps.Transitions != nil {
		_, _ = deps.Transitions.Append(ctx, workflowID, state.Transition{
			WorkflowID:     workflowID,
			Status:         state.StatusRunning,
			PhaseFrom:      state.PhaseIntake,
			PhaseTo:        state.PhaseIntake,
			OccurredAt:     now,
			EnvelopeDigest: envelopeDigest,
		})
	}
	return out, nil
}

func envelopeBudgetScope(env *ExecutionEnvelope) string {
	if env == nil {
		return ""
	}
	return env.Cost.BudgetScope
}

func envelopeBudgetScopeID(env *ExecutionEnvelope) string {
	if env == nil {
		return ""
	}
	return env.Cost.BudgetScopeID
}

func laneOrDefault(lane string) string {
	if lane == "" {
		return string(defaultLane)
	}
	return lane
}

// TenXWorkflowID derives a deterministic workflow ID for a 10x delivery from a
// caller-supplied idempotency key.
func TenXWorkflowID(idempotencyKey string) string {
	sum := sha256.Sum256([]byte("tenx|" + idempotencyKey))
	return "tenx-" + hex.EncodeToString(sum[:])[:32]
}

// StartTenXDeliveryInput names the org path's single production trigger inputs.
type StartTenXDeliveryInput struct {
	IdempotencyKey string
	Lane           string
	Workflow       TenXDeliverInput
}

// StartTenXDelivery starts one TenXDeliver execution on the lane-resolved task
// queue with a deterministic, idempotent workflow ID — the org path's
// equivalent of StartDelivery (docs/PLAN.md Task 108 / RTC-04). It shares the
// same single-production-trigger discipline: the kernel resolves the lane and
// the workflow ID (Constitution C4).
func StartTenXDelivery(ctx context.Context, deps StartDeps, in StartTenXDeliveryInput) (StartDeliveryOutput, error) {
	if deps.Starter == nil {
		return StartDeliveryOutput{}, fmt.Errorf("kernel: StartTenXDelivery requires a workflow starter")
	}
	queue, err := deps.LaneSelector.Select(in.Lane, deps.QueueConfig)
	if err != nil {
		return StartDeliveryOutput{}, fmt.Errorf("%w: %v", ErrStartRefused, err)
	}
	workflowID := TenXWorkflowID(in.IdempotencyKey)
	run, err := deps.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             queue,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, TenXDeliver, in.Workflow)
	if err != nil {
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &already) {
			return StartDeliveryOutput{}, fmt.Errorf("%w: workflow %s", ErrStartDuplicate, workflowID)
		}
		return StartDeliveryOutput{}, fmt.Errorf("kernel: start tenx workflow %s: %w", workflowID, err)
	}
	return StartDeliveryOutput{WorkflowID: workflowID, RunID: run.GetRunID(), TaskQueue: queue, Lane: laneOrDefault(in.Lane)}, nil
}
