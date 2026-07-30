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
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
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
// (Constitution C4).
type StartDeliveryInput struct {
	PlanID       string
	PlanFilePath string
	RepoPath     string
	Lane         string
	Attempt      int
}

// StartDeliveryOutput reports what the kernel resolved and started.
type StartDeliveryOutput struct {
	WorkflowID string
	RunID      string
	TaskQueue  string
	Lane       string
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
}

// DeliveryWorkflowID derives the deterministic workflow ID from the plan
// digest and attempt ordinal, so a double-click, a retried HTTP request and a
// Telegram retry all collapse to one execution rather than three.
func DeliveryWorkflowID(planDigest string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", planDigest, attempt)))
	return "deliver-" + hex.EncodeToString(sum[:])[:32]
}

// StartDelivery is Foundry's single production edge from an ApprovedPlan to a
// running DeliverPlan execution (docs/PLAN.md Task 105 / RTC-01). The kernel —
// never the transport — resolves the lane, the executor allowlist and the
// workflow ID (Constitution C4).
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

	// (3) The executor allowlist must be non-nil and non-empty. A nil/empty
	// allowlist is the fail-open path Task 116 closes — refuse rather than run
	// with no policy (Constitution C4).
	if len(deps.ExecutorAllowlist) == 0 {
		return StartDeliveryOutput{}, fmt.Errorf("%w: empty executor allowlist", ErrStartRefused)
	}

	// (4) Deterministic, idempotent workflow ID.
	workflowID := DeliveryWorkflowID(approved.PlanDigest(), in.Attempt)

	run, err := deps.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             queue,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, DeliverPlan, DeliverPlanInput{
		PlanID:            in.PlanID,
		PlanFilePath:      in.PlanFilePath,
		RepoPath:          in.RepoPath,
		ExecutorAllowlist: deps.ExecutorAllowlist,
	})
	if err != nil {
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &already) {
			return StartDeliveryOutput{}, fmt.Errorf("%w: workflow %s", ErrStartDuplicate, workflowID)
		}
		return StartDeliveryOutput{}, fmt.Errorf("kernel: start workflow %s: %w", workflowID, err)
	}

	out := StartDeliveryOutput{
		WorkflowID: workflowID,
		RunID:      run.GetRunID(),
		TaskQueue:  queue,
		Lane:       laneOrDefault(in.Lane),
	}

	// (5) Record an initial transition so `foundry status` sees the execution
	// immediately (best-effort: a transition-store failure does not undo a
	// started workflow).
	if deps.Transitions != nil {
		_, _ = deps.Transitions.Append(ctx, workflowID, state.Transition{
			WorkflowID: workflowID,
			Status:     state.StatusRunning,
			PhaseFrom:  state.PhaseIntake,
			PhaseTo:    state.PhaseIntake,
			OccurredAt: time.Now().UTC(),
		})
	}
	return out, nil
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
