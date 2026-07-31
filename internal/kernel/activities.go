package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// heartbeatInterval is how often ExecuteTask reports liveness to Temporal
// while the executor adapter runs (docs/PLAN.md Task 12 Step: "heartbeats
// every 10s").
const heartbeatInterval = 10 * time.Second

// Activities bundles every side-effecting operation DeliverPlan calls out
// to. It is the only place in this package that touches the world —
// workflow.go must never construct or call these directly outside of
// workflow.ExecuteActivity.
type Activities struct {
	ProvenanceStore *provenance.Store
	WorktreeManager *worktree.Manager
	EvidenceStore   evidence.Store
	LeaseStore      LeaseStore
	ReceiptStore    ReceiptStore
	TransitionStore TransitionStore
	CostStore       BudgetStore
	CostDefaults    cost.Defaults
	// Validator runs a task's declared validation commands and reports
	// their real, evidence-grade outcome (docs/PLAN.md Task 13 /
	// internal/verify). ValidateTask classifies pass/fail from this, never
	// from ExecuteTaskOutput's executor-self-report (Constitution C10).
	Validator verify.Runner

	// ExecutorSelector and CapabilityRegistry are the kernel-owned executor
	// selection inputs (docs/PLAN.md Task 85 / PRV-02, Constitution C4).
	// When a task's ExecuteTaskInput carries a non-nil ExecutorAllowlist,
	// ExecuteTask runs Select to decide (and policy-check) which adapter
	// runs the task instead of using ExecutorName unchecked. Both are
	// zero-valued by default; that preserves the pre-Task-85 unchecked
	// lookup for callers (e.g. unit tests) that supply no allowlist.
	ExecutorSelector   ExecutorSelector
	CapabilityRegistry capability.Registry

	// Sandbox is the mandatory-sandbox execution seam (docs/PLAN.md Task 115 /
	// SEC-01). When a task's ExecuteTaskInput.RequireSandbox is set, ExecuteTask
	// runs the resolved executor inside this sandbox and refuses to execute at
	// all when it is unavailable — never a host fallback (C24). Zero-valued by
	// default so pre-Task-115 callers (RequireSandbox=false) are unaffected.
	Sandbox SandboxRunner

	// Integrator and IntegrationQueue back the 10x IntegrateChangeSet activity
	// (docs/PLAN.md Task 108 / RTC-04). Both are zero-valued by default; only
	// the 10x path sets them, so DeliverPlan callers are unaffected.
	Integrator       *integrator.Integrator
	IntegrationQueue IntegrationQueue

	mu         sync.Mutex
	workspaces map[string]worktree.Workspace
}

// NewActivities builds an Activities set from its dependencies.
func NewActivities(
	provenanceStore *provenance.Store,
	worktreeManager *worktree.Manager,
	evidenceStore evidence.Store,
	leaseStore LeaseStore,
	receiptStore ReceiptStore,
	transitionStore TransitionStore,
	costStore BudgetStore,
	costDefaults cost.Defaults,
	validator verify.Runner,
) *Activities {
	return &Activities{
		ProvenanceStore: provenanceStore,
		WorktreeManager: worktreeManager,
		EvidenceStore:   evidenceStore,
		LeaseStore:      leaseStore,
		ReceiptStore:    receiptStore,
		TransitionStore: transitionStore,
		CostStore:       costStore,
		CostDefaults:    costDefaults,
		Validator:       validator,
		workspaces:      make(map[string]worktree.Workspace),
	}
}

func workspaceKey(workflowID, taskID string) string { return workflowID + "/" + taskID }

func (a *Activities) storeWorkspace(workflowID, taskID string, ws worktree.Workspace) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.workspaces[workspaceKey(workflowID, taskID)] = ws
}

func (a *Activities) loadWorkspace(workflowID, taskID string) (worktree.Workspace, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, ok := a.workspaces[workspaceKey(workflowID, taskID)]
	return ws, ok
}

func (a *Activities) deleteWorkspace(workflowID, taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.workspaces, workspaceKey(workflowID, taskID))
}

// LoadApprovedPlanInput is LoadApprovedPlan's input.
type LoadApprovedPlanInput struct {
	PlanID       string
	PlanFilePath string
}

// LoadApprovedPlanOutput is LoadApprovedPlan's output.
type LoadApprovedPlanOutput struct {
	PlanID   string
	RiskTier string
	Tasks    []plan.Task
}

// LoadApprovedPlan verifies the on-disk plan file at in.PlanFilePath still
// matches its ApprovedPlan's signed digest (internal/provenance,
// Constitution C7) and that its granted permissions are still a subset of
// what was requested, then parses the plan's task list. A tampered file, a
// forged/corrupted stored approval, or a granted-permission escape all
// surface as an error here — deterministic failures that must not retry.
func (a *Activities) LoadApprovedPlan(ctx context.Context, in LoadApprovedPlanInput) (LoadApprovedPlanOutput, error) {
	result, err := provenance.VerifyPlanFile(ctx, a.ProvenanceStore, in.PlanID, in.PlanFilePath)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: load approved plan %s: %w", in.PlanID, err)
	}
	if !result.DigestMatches {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: plan file %s no longer matches its approved digest", in.PlanFilePath)
	}
	if !result.GrantedSubset {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: approved plan %s grants permissions outside requested scope", in.PlanID)
	}

	approved, err := a.ProvenanceStore.Load(ctx, in.PlanID)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: reload approved plan %s: %w", in.PlanID, err)
	}

	raw, err := os.ReadFile(in.PlanFilePath)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: read plan file %s: %w", in.PlanFilePath, err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: parse plan file %s: %w", in.PlanFilePath, err)
	}

	return LoadApprovedPlanOutput{PlanID: in.PlanID, RiskTier: approved.RiskTier(), Tasks: doc.Tasks}, nil
}

// RecheckApprovalInput is RecheckApproval's input.
type RecheckApprovalInput struct {
	PlanID string
}

// RecheckApprovalOutput is RecheckApproval's output.
type RecheckApprovalOutput struct {
	OK bool
}

// RecheckApproval re-verifies the plan's ApprovedPlan is still signed,
// unexpired, and unrevoked (Constitution C7 rule 5: "execution re-checks
// revocation at every wave boundary"). It is called by DeliverPlan before
// every task, not just once at admission — a plan revoked mid-flight
// (docs/PLAN.md Task 24 Acceptance) must stop the workflow before its
// next task starts. It deliberately reuses ProvenanceStore.Load rather
// than duplicating the expiry/revocation check: the same choke point that
// enforces C7 for the initial LoadApprovedPlan enforces it here too, so
// there is exactly one place a revocation can be missed, not two.
func (a *Activities) RecheckApproval(ctx context.Context, in RecheckApprovalInput) (RecheckApprovalOutput, error) {
	if _, err := a.ProvenanceStore.Load(ctx, in.PlanID); err != nil {
		return RecheckApprovalOutput{}, fmt.Errorf("kernel: recheck approval %s: %w", in.PlanID, err)
	}
	return RecheckApprovalOutput{OK: true}, nil
}

// ReserveBudgetInput is ReserveBudget's input. Attempt distinguishes a
// genuinely new logical attempt (e.g. this task is being retried after a
// WAITING/budget pause was resolved by `foundry budget raise`) from a
// Temporal-level re-execution of the same attempt — only the former must
// bypass the idempotency receipt and actually call Reserve again; reusing
// a fixed Attempt across a budget-triggered retry would replay the stale
// exhausted result forever instead of re-checking the (now-raised)
// envelope.
type ReserveBudgetInput struct {
	WorkflowID   string
	TaskID       string
	ExecutorName string
	Attempt      int
	// MissionID, when set, aligns the reservation scope with the scope a
	// mission's budget is actually provisioned at (docs/PLAN.md Task 119 /
	// COST-01): the kernel previously reserved at ScopeWorkflow/WorkflowID
	// while mission budgets are provisioned and read at ScopeMission/missionID,
	// so a correctly-provisioned mission envelope was never consulted. When
	// MissionID is set the reservation is made at ScopeMission/MissionID.
	MissionID string
	// Unattended marks a reservation for an unattended (autonomous) mission.
	// An unattended reservation with no budget envelope is REFUSED, never run
	// unmetered (C19/C24). An attended/interactive reservation without an
	// envelope stays unmetered, preserving interactive use.
	Unattended bool
}

// ReserveBudgetOutput is ReserveBudget's output. Exhausted reports a
// business-level budget exhaustion (Constitution C19) — not an activity
// execution fault — so DeliverPlan can pause to WAITING/budget rather
// than fail the whole plan, mirroring how ExecuteTaskOutput.Failed
// reports the executor's own outcome as data rather than a Go error.
type ReserveBudgetOutput struct {
	EntryID   string
	Exhausted bool
	Shadow    bool
	// Refused reports a fail-closed refusal (Task 119): an unattended mission
	// with no budget envelope must not execute. Classification names why.
	Refused        bool
	Classification string
}

// ReserveBudget reserves this task's estimated cost against its
// workflow-scoped mission_monthly budget envelope before ExecuteTask runs
// (Constitution C19: "Before execution: reserve expected spend ... reject
// or shrink the work when the reservation cannot be satisfied"). The
// estimate is a.CostDefaults.DefaultUSD for every task — see
// internal/ledger/cost/defaults.go's doc comment for why this task does
// not yet read a per-task declared estimate. This reservation amount also
// serves as the per-task session cap (docs/PLAN.md Task 29 Step 5):
// exceeding it is exactly what turns Exhausted true, cancelling the task
// rather than letting the executor run unmetered — deep, live per-call
// metering inside internal/executor's Run loop does not exist anywhere
// today, so a coarser "reserve the estimate before running, over-budget
// means don't run" is this task's whole enforcement point.
//
// Subscription-class executors (isSubscriptionExecutor) have no metered
// per-call price to reserve against, so their cost is instead recorded as
// a state=shadow cost_entries row (cost-accounting.md §1) with no ceiling
// check at all — Shadow reports that path was taken.
//
// A workflow scope with no provisioned envelope (Reserve returns
// cost.ErrBudgetNotFound) is treated as unmetered. decision (no-gaps
// rule): requiring every workflow to have a budget provisioned before it
// can run at all is out of this task's scope — only scopes an operator
// has actually configured via `foundry budget raise`/CreateBudget are
// enforced; everything else runs exactly as it did before this task.
func (a *Activities) ReserveBudget(ctx context.Context, in ReserveBudgetInput) (ReserveBudgetOutput, error) {
	key := IdempotencyKey{in.WorkflowID, in.TaskID, "ReserveBudget", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (ReserveBudgetOutput, error) {
		amountUSD := a.CostDefaults.DefaultUSD
		meta := map[string]string{"task_id": in.TaskID}

		// Task 119 (COST-01): align the reservation scope with the scope the
		// budget is actually provisioned at. A mission task reserves at
		// ScopeMission/MissionID (where mission budgets live), not
		// ScopeWorkflow/WorkflowID (where nothing was ever provisioned).
		scope, scopeID := cost.ScopeWorkflow, in.WorkflowID
		if in.MissionID != "" {
			scope, scopeID = cost.ScopeMission, in.MissionID
		}

		if isSubscriptionExecutor(in.ExecutorName) {
			entry, err := a.CostStore.RecordShadow(ctx, scope, scopeID, amountUSD, in.ExecutorName, costPricingVersion, meta)
			if err != nil {
				return ReserveBudgetOutput{}, fmt.Errorf("kernel: record shadow cost %s/%s: %w", in.WorkflowID, in.TaskID, err)
			}
			observe.ObserveCostPerTask(in.ExecutorName, entry.AmountUSD)
			return ReserveBudgetOutput{EntryID: entry.ID, Shadow: true}, nil
		}

		entry, err := a.CostStore.Reserve(ctx, scope, scopeID, cost.KindMissionMonthly, currentPeriod(time.Now()), amountUSD, in.ExecutorName, costPricingVersion, meta)
		switch {
		case errors.Is(err, cost.ErrBudgetExhausted):
			return ReserveBudgetOutput{Exhausted: true}, nil
		case errors.Is(err, cost.ErrBudgetNotFound):
			// Task 119 (COST-01): "no envelope" is a REFUSAL for an unattended
			// mission, not "unmetered". Attended/interactive use without an
			// envelope stays unmetered (a human is present).
			if in.Unattended {
				return ReserveBudgetOutput{
					Refused:        true,
					Classification: "budget-envelope-absent",
				}, nil
			}
			return ReserveBudgetOutput{}, nil
		case err != nil:
			return ReserveBudgetOutput{}, fmt.Errorf("kernel: reserve budget %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		observe.ObserveCostPerTask(in.ExecutorName, entry.AmountUSD)
		return ReserveBudgetOutput{EntryID: entry.ID}, nil
	})
}

// AcquireLeaseInput is AcquireLease's input.
type AcquireLeaseInput struct {
	Resource   string
	Holder     string
	TTLSeconds int
}

// AcquireLeaseOutput is AcquireLease's output.
type AcquireLeaseOutput struct {
	Token string
}

// AcquireLease grants (or idempotently re-grants) a fencing token for
// Resource. A conflicting live holder is a deterministic ErrLeaseHeld —
// callers must not retry it blindly.
func (a *Activities) AcquireLease(ctx context.Context, in AcquireLeaseInput) (AcquireLeaseOutput, error) {
	lease, err := a.LeaseStore.Acquire(ctx, in.Resource, in.Holder, time.Duration(in.TTLSeconds)*time.Second)
	if err != nil {
		return AcquireLeaseOutput{}, err
	}
	return AcquireLeaseOutput{Token: lease.Token}, nil
}

// AcquireWorktreeInput is AcquireWorktree's input.
type AcquireWorktreeInput struct {
	WorkflowID    string
	TaskID        string
	Attempt       int
	RepoPath      string
	LeaseResource string
	LeaseToken    string
}

// AcquireWorktreeOutput is AcquireWorktree's output.
type AcquireWorktreeOutput struct {
	Path   string
	Branch string
}

// AcquireWorktree checks the fencing token is still current for
// LeaseResource, then creates an isolated worktree via internal/worktree.
// Re-execution of an already-completed acquisition (same workflow/task/
// attempt) returns the recorded receipt instead of creating a second
// worktree.
func (a *Activities) AcquireWorktree(ctx context.Context, in AcquireWorktreeInput) (AcquireWorktreeOutput, error) {
	// retry_rate (docs/PLAN.md Task 31): activity.Info.Attempt is
	// Temporal's own per-call attempt counter (opts.retry allows up to 3
	// here — workflow.go), distinct from in.Attempt, this repo's
	// workflow-level logical-attempt field (internal/kernel/idempotency.go).
	// A real failure classification per docs/foundry/docs/operations/
	// observability-and-alerts.md's "per failure classification" note is
	// Task 32's retry-policy engine, not built yet — recorded as "" until
	// then rather than fabricated.
	observe.RecordActivityAttempt("AcquireWorktree", temporalAttempt(ctx), "")

	ok, err := a.LeaseStore.Check(ctx, in.LeaseResource, in.LeaseToken)
	if err != nil {
		return AcquireWorktreeOutput{}, fmt.Errorf("kernel: check lease %s: %w", in.LeaseResource, err)
	}
	if !ok {
		return AcquireWorktreeOutput{}, fmt.Errorf("%w: fencing token no longer valid for %s", ErrLeaseHeld, in.LeaseResource)
	}

	key := IdempotencyKey{in.WorkflowID, in.TaskID, "AcquireWorktree", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (AcquireWorktreeOutput, error) {
		ws, err := a.WorktreeManager.Acquire(ctx, in.RepoPath, in.WorkflowID, in.TaskID)
		if err != nil {
			return AcquireWorktreeOutput{}, fmt.Errorf("kernel: acquire worktree %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		a.storeWorkspace(in.WorkflowID, in.TaskID, ws)
		return AcquireWorktreeOutput{Path: ws.Path, Branch: ws.Branch}, nil
	})
}

// ReleaseWorktreeInput is ReleaseWorktree's input.
type ReleaseWorktreeInput struct {
	WorkflowID string
	TaskID     string
}

// ReleaseWorktree reclaims the worktree acquired for (WorkflowID, TaskID).
// It is best-effort against the in-process cache populated by
// AcquireWorktree in the same worker process: if the worker restarted in
// between and the cache is empty, this is a no-op — the orphan is still
// eventually reclaimed by worktree.Manager.SweepOlderThan, which is this
// package's safety net rather than something workflow.go depends on
// (decision: smallest reversible option, since Workspace.Release is a
// closure that cannot cross an activity's serialization boundary).
func (a *Activities) ReleaseWorktree(_ context.Context, in ReleaseWorktreeInput) error {
	ws, ok := a.loadWorkspace(in.WorkflowID, in.TaskID)
	if !ok || ws.Release == nil {
		return nil
	}
	defer a.deleteWorkspace(in.WorkflowID, in.TaskID)
	if err := ws.Release(); err != nil {
		return fmt.Errorf("kernel: release worktree %s/%s: %w", in.WorkflowID, in.TaskID, err)
	}
	return nil
}

// ExecuteTaskInput is ExecuteTask's input.
type ExecuteTaskInput struct {
	WorkflowID    string
	TaskID        string
	Attempt       int
	ExecutorName  string
	WorkspacePath string
	Packet        executor.TaskPacket
	// ExplicitExecutor is the plan task's own named executor (plan.Task.
	// Executor), or empty when the task names none. Consulted only when
	// ExecutorAllowlist is non-nil (docs/PLAN.md Task 85 / PRV-02).
	ExplicitExecutor string
	// TaskClass is the plan task's routing class (plan.Task.Class), used by
	// the selector's routing table (Task 90) when no executor is explicit.
	TaskClass string
	// ExecutorAllowlist is the resolved policy's executor_allowlist. When
	// nil, ExecuteTask keeps the pre-Task-85 unchecked lookup of
	// ExecutorName; when non-nil (even if empty), ExecuteTask runs
	// kernel-owned, policy-checked selection and fails closed on violation.
	ExecutorAllowlist []string
	// RequireSandbox is set from the task's resolved profile policy when that
	// policy demands the mandatory executor sandbox (docs/PLAN.md Task 115 /
	// SEC-01). When true, ExecuteTask runs the executor inside the sandbox and
	// refuses (named classification) rather than falling back to host
	// execution if the sandbox is unavailable or the executor is incompatible.
	// A rollback cannot set this false for a profile whose policy demands
	// sandboxing — the workflow derives it from policy, not from a flag.
	RequireSandbox bool
}

// ExecuteTaskOutput is ExecuteTask's output. Failed/ErrorMessage carry the
// executor adapter's own (still untrusted) pass/fail — a task whose
// commands failed is a valid business outcome, not an activity execution
// fault, so it is reported here rather than as a Go error (that keeps
// Temporal's activity-level retry policy scoped to genuine infra faults —
// executor lookup, Prepare, Collect — not to deterministic script
// failures).
type ExecuteTaskOutput struct {
	Claimed       string
	ExitNotes     string
	Failed        bool
	ErrorMessage  string
	ArtifactPaths []string
	// ExecutorUsed is the executor name selection actually chose for this
	// task (docs/PLAN.md Task 85 Step 3 — recorded on the evidence bundle).
	ExecutorUsed string
	// Classification, when set, is the deterministic verify.Classification
	// of a pre-execution fail-closed outcome (today: an executor-selection
	// policy violation). It is surfaced to ValidateTask as the task's real
	// classification instead of the generic executor-failed default.
	Classification string
}

// ExecuteTask runs packet inside the already-acquired worktree via the
// named executor.Adapter, heartbeating every 10s so Temporal knows the
// activity is alive during a long-running task.
func (a *Activities) ExecuteTask(ctx context.Context, in ExecuteTaskInput) (ExecuteTaskOutput, error) {
	observe.RecordActivityAttempt("ExecuteTask", temporalAttempt(ctx), "")

	key := IdempotencyKey{in.WorkflowID, in.TaskID, "ExecuteTask", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (ExecuteTaskOutput, error) {
		// Task 85 (PRV-02, Constitution C4): the kernel — not an unchecked
		// env var, not PEC, not the executor itself — decides which adapter
		// runs, and validates that decision against the policy allowlist and
		// capability registry. When no allowlist is supplied (pre-Task-85
		// callers), fall back to the historical unchecked lookup.
		execName := in.ExecutorName
		if in.ExecutorAllowlist != nil {
			task := plan.Task{ID: in.TaskID, Executor: in.ExplicitExecutor, Class: in.TaskClass}
			pol := compiler.Resolved{Effective: compiler.Policy{ExecutorAllowlist: in.ExecutorAllowlist}}
			selected, selErr := a.ExecutorSelector.Select(ctx, task, pol, a.CapabilityRegistry)
			if selErr != nil {
				var se *SelectionError
				if errors.As(selErr, &se) {
					return ExecuteTaskOutput{
						Failed:         true,
						ErrorMessage:   se.Error(),
						ExecutorUsed:   se.Executor,
						Classification: string(se.Classification),
					}, nil
				}
				return ExecuteTaskOutput{}, fmt.Errorf("kernel: select executor %s/%s: %w", in.WorkflowID, in.TaskID, selErr)
			}
			execName = selected
		}

		adapter, err := executor.Get(execName)
		if err != nil {
			return ExecuteTaskOutput{}, fmt.Errorf("kernel: execute task %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}

		ws := worktree.Workspace{Path: in.WorkspacePath}
		if err := adapter.Prepare(ctx, ws, in.Packet); err != nil {
			return ExecuteTaskOutput{}, fmt.Errorf("kernel: prepare task %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}

		// Task 115 (SEC-01): decide whether this task must run sandboxed. A
		// sandbox-required task that cannot be sandboxed is refused with a named
		// classification — never a host fallback (C24).
		decision := a.decideSandbox(execName, in.RequireSandbox)
		if decision.refusal != "" {
			return refuseSandbox(execName, decision.refusal, decision.reason), nil
		}

		// provider_waiting_time (docs/PLAN.md Task 31): STUB per the card
		// ("stub source is acceptable") — adapter.Run's wall-clock duration
		// conflates real provider wait time with the adapter's own local
		// work; see observe.ProviderWaitingTimeSeconds's doc comment.
		runStart := time.Now()
		stopHeartbeat := startHeartbeat(ctx)
		var summary executor.Summary
		var runErr error
		if decision.required {
			// Run INSIDE the sandbox. A refusal (unwired/unavailable/incompatible
			// sandbox) or a sandbox run failure returns fail-closed here.
			var out ExecuteTaskOutput
			var okRun bool
			summary, out, okRun = a.runSandboxed(ctx, execName, adapter, ws, in.Packet)
			if !okRun {
				stopHeartbeat()
				observe.ObserveProviderWaitingTime(execName, time.Since(runStart).Seconds())
				return out, nil
			}
		} else {
			summary, runErr = adapter.Run(ctx)
		}
		stopHeartbeat()
		observe.ObserveProviderWaitingTime(execName, time.Since(runStart).Seconds())

		out := ExecuteTaskOutput{Claimed: summary.Claimed, ExitNotes: summary.ExitNotes, ExecutorUsed: execName}
		if runErr != nil {
			out.Failed = true
			out.ErrorMessage = runErr.Error()
			return out, nil
		}

		artifacts, err := adapter.Collect(ctx)
		if err != nil {
			return ExecuteTaskOutput{}, fmt.Errorf("kernel: collect task %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		out.ArtifactPaths = artifacts.Paths
		return out, nil
	})
}

// temporalAttempt returns ctx's Temporal activity.Info.Attempt, or 1 (a
// harmless "first attempt" default that observe.RecordActivityAttempt
// treats as a no-op) when ctx is not a real Temporal activity context —
// e.g. this package's own unit tests, which call Activities methods
// directly against context.Background() (see idempotency_test.go).
// activity.GetInfo panics outside a real activity context, so
// activity.IsActivity guards the call rather than relying on recover.
func temporalAttempt(ctx context.Context) int32 {
	if !activity.IsActivity(ctx) {
		return 1
	}
	return activity.GetInfo(ctx).Attempt
}

// startHeartbeat records an activity heartbeat every heartbeatInterval
// until the returned stop function is called or ctx is done. Called from
// inside an activity, so activity.RecordHeartbeat is safe here (it would
// not be safe from workflow code).
func startHeartbeat(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "running")
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(done) }
}

// ValidateTaskInput is ValidateTask's input. WorkspacePath/
// ValidationCommands are only consulted when ExecuteFailed is false — see
// ValidateTask's doc comment. Attempt is the 1-indexed number of times
// this same task's validation has now run (mirrors ReserveBudgetInput's
// own budget-retry Attempt — see runTask), and is forwarded to
// verify.Evaluate to distinguish a timeout's first occurrence from a
// repeat.
type ValidateTaskInput struct {
	ExecuteFailed      bool
	WorkspacePath      string
	ValidationCommands []string
	Attempt            int
	// PreClassification, when set alongside ExecuteFailed, is a
	// classification already decided before execution (today: an executor-
	// selection policy violation, docs/PLAN.md Task 85). ValidateTask
	// surfaces it verbatim instead of the generic executor-failed default,
	// so a fail-closed selection reports "policy-violation", not
	// "verification-failed".
	PreClassification string
}

// ValidateTaskOutput is ValidateTask's output.
type ValidateTaskOutput struct {
	Validated bool
	Reason    string
}

// ValidateTask is the sole place a task's pass/fail verdict is decided
// (Constitution C10: honest completion comes from commands actually run,
// never from an executor's self-reported claim). ExecuteFailed
// short-circuits to today's fast path — ExecuteTask's own adapter run
// already reported failure, so there is nothing further to validate.
// Otherwise it runs in.ValidationCommands for real via a.Validator.Run and
// classifies the outcome from the resulting CommandRecords via
// verify.Evaluate — the executor's Summary/claimed success is never
// consulted here (docs/PLAN.md Task 13's honest-completion contract,
// wired into the kernel workflow by Task 99/SKP-11R).
func (a *Activities) ValidateTask(ctx context.Context, in ValidateTaskInput) (ValidateTaskOutput, error) {
	if in.ExecuteFailed {
		classification := in.PreClassification
		if classification == "" {
			classification = "verification-failed"
		}
		observe.RecordEvidenceResult(false, classification)
		return ValidateTaskOutput{Validated: false, Reason: classification}, nil
	}

	records, err := a.Validator.Run(ctx, worktree.Workspace{Path: in.WorkspacePath}, in.ValidationCommands)
	if err != nil {
		return ValidateTaskOutput{}, fmt.Errorf("kernel: validate task: %w", err)
	}

	ok, classification := verify.Evaluate(records, in.Attempt)
	if ok {
		observe.RecordEvidenceResult(true, "")
		return ValidateTaskOutput{Validated: true}, nil
	}
	observe.RecordEvidenceResult(false, string(classification))
	return ValidateTaskOutput{Validated: false, Reason: string(classification)}, nil
}

// RecordEvidenceInput is RecordEvidence's input.
type RecordEvidenceInput struct {
	WorkflowID    string
	TaskID        string
	Attempt       int
	WorkspacePath string
	ArtifactPaths []string
	ExecuteFailed bool
	// ExecutorUsed is the executor name selection chose for this task
	// (docs/PLAN.md Task 85 Step 3), recorded on the evidence manifest so
	// every task's bundle names which adapter actually ran.
	ExecutorUsed string
}

// RecordEvidenceOutput is RecordEvidence's output.
type RecordEvidenceOutput struct {
	BundleID string
}

// RecordEvidence hashes the artifacts ExecuteTask collected, builds an
// evidence.Manifest, and persists it via the evidence.Store. Bundles are
// content-addressed (Task 11): re-recording identical evidence returns the
// existing bundle's ID rather than erroring.
func (a *Activities) RecordEvidence(ctx context.Context, in RecordEvidenceInput) (RecordEvidenceOutput, error) {
	observe.RecordActivityAttempt("RecordEvidence", temporalAttempt(ctx), "")

	key := IdempotencyKey{in.WorkflowID, in.TaskID, "RecordEvidence", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (RecordEvidenceOutput, error) {
		artifacts := make([]evidence.ArtifactRef, 0, len(in.ArtifactPaths))
		for _, p := range in.ArtifactPaths {
			full := filepath.Join(in.WorkspacePath, p)
			sum, size, err := hashArtifact(full)
			if err != nil {
				return RecordEvidenceOutput{}, fmt.Errorf("kernel: hash artifact %s: %w", p, err)
			}
			artifacts = append(artifacts, evidence.ArtifactRef{Path: p, SHA256: sum, Bytes: size})
		}

		exitCode := 0
		if in.ExecuteFailed {
			exitCode = 1
		}
		manifest := evidence.Manifest{
			WorkflowID:   in.WorkflowID,
			TaskID:       in.TaskID,
			ExecutorUsed: in.ExecutorUsed,
			Commands: []evidence.CommandRecord{{
				Cmd:      "executor.Run", // coarse record: Task 13's Runner replaces this with per-command records.
				ExitCode: exitCode,
			}},
			Artifacts: artifacts,
			CreatedAt: time.Now().UTC(),
		}

		id, err := manifest.DigestHex()
		if err != nil {
			return RecordEvidenceOutput{}, fmt.Errorf("kernel: compute evidence digest %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}

		putID, err := a.EvidenceStore.Put(evidence.Bundle{Manifest: manifest, Dir: in.WorkspacePath})
		if err != nil {
			if errors.Is(err, evidence.ErrBundleExists) {
				return RecordEvidenceOutput{BundleID: id}, nil
			}
			return RecordEvidenceOutput{}, fmt.Errorf("kernel: record evidence %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		return RecordEvidenceOutput{BundleID: putID}, nil
	})
}

func hashArtifact(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// AppendTransitionInput is AppendTransition's input. TaskID scopes the
// idempotency key only — it is not part of the persisted state.Transition
// itself.
type AppendTransitionInput struct {
	WorkflowID string
	TaskID     string
	Attempt    int
	Transition state.Transition
}

// AppendTransitionOutput is AppendTransition's output.
type AppendTransitionOutput struct {
	Seq int64
}

// AppendTransition durably persists one canonical state.Transition to the
// workflow_transitions stream — the source of Task 14's projection.
func (a *Activities) AppendTransition(ctx context.Context, in AppendTransitionInput) (AppendTransitionOutput, error) {
	key := IdempotencyKey{in.WorkflowID, in.TaskID, "AppendTransition", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (AppendTransitionOutput, error) {
		seq, err := a.TransitionStore.Append(ctx, in.WorkflowID, in.Transition)
		if err != nil {
			return AppendTransitionOutput{}, fmt.Errorf("kernel: append transition %s: %w", in.WorkflowID, err)
		}
		// workflow_completion_rate (docs/PLAN.md Task 31): recorded inside
		// this closure, not after withReceipt returns, so a receipt hit
		// (this activity re-invoked for a key already recorded — e.g. a
		// worker crash/redelivery of the same attempt) can never
		// double-count it; this closure body runs at most once per
		// (workflow, task, attempt) key by withReceipt's own contract.
		if in.Transition.Status.IsTerminal() {
			observe.RecordWorkflowCompletion(string(in.Transition.Status))
		}
		return AppendTransitionOutput{Seq: seq}, nil
	})
}
