package kernel

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/pec"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// Activity names, registered by cmd/foundryd/main.go and referenced here
// by name so workflow.go never imports the Activities struct itself
// (workflow code must stay decoupled from activity implementations —
// only worker.go wires them together).
const (
	ActivityLoadApprovedPlan = "LoadApprovedPlan"
	ActivityRecheckApproval  = "RecheckApproval"
	ActivityReserveBudget    = "ReserveBudget"
	ActivityAcquireLease     = "AcquireLease"
	ActivityAcquireWorktree  = "AcquireWorktree"
	ActivityReleaseWorktree  = "ReleaseWorktree"
	ActivityExecuteTask      = "ExecuteTask"
	ActivityValidateTask     = "ValidateTask"
	ActivityRecordEvidence   = "RecordEvidence"
	ActivityAppendTransition = "AppendTransition"
	// ActivityRecordCost incurs a completed task's real cost against its
	// reservation (docs/PLAN.md Task 120 / COST-02).
	ActivityRecordCost = "RecordCost"
)

// defaultTaskTimeout bounds a single task's ExecuteTask activity. plan.Task
// (Task 6's schema) carries no per-task timeout field, so this package
// applies one fixed default for every task rather than inventing a new
// plan-schema field out of scope for this task (decision: smallest
// reversible option per the no-gaps rule; a future task can add a
// per-task TimeoutSec to plan.Task without changing this package's
// behavior for plans that don't set it).
const defaultTaskTimeout = 10 * time.Minute

// Venture-loop phase letters (docs/foundry/docs/workflows/venture-loop.md).
// PhaseHint reuses this repo's own existing lettering — no new
// discuss/plan/build/verify/ship taxonomy is introduced (docs/PLAN.md Task
// 92 / PRV-09).
const (
	phaseExecution = "I" // venture-loop phase I: task execution
)

// phaseHintFor is the kernel's pure, non-authoritative derivation of a
// task's PhaseHint (docs/PLAN.md Task 92 / PRV-09). ExecuteTask is, by
// definition, the execution phase (venture-loop I), so that is the hint the
// kernel forwards. It introduces no new decision point: the value is never
// read back by any kernel decision path (ExecutorSelector, ValidateTask,
// admission) — TestPhaseHintNeverRead enforces that. Deterministic (no
// time/rand), safe in workflow code.
func phaseHintFor(_ plan.Task) string {
	return phaseExecution
}

// defaultLeaseTTL bounds how long a worktree fencing token is valid before
// it can be reclaimed by a new holder.
const defaultLeaseTTL = 15 * time.Minute

// DeliverPlanInput is DeliverPlan's workflow input.
type DeliverPlanInput struct {
	PlanID       string
	PlanFilePath string
	RepoPath     string
	ExecutorName string
	// ExecutorAllowlist is the resolved policy's executor_allowlist. When
	// non-nil, the kernel runs policy-checked, capability-registry-gated
	// executor selection for every task (docs/PLAN.md Task 85 / PRV-02,
	// Constitution C4). When nil, ExecuteTask keeps the historical unchecked
	// lookup of ExecutorName (used by callers that don't resolve policy).
	ExecutorAllowlist []string
}

// TaskResult reports one task's outcome within the plan.
type TaskResult struct {
	TaskID string
	Failed bool
}

// DeliverPlanResult is DeliverPlan's workflow result.
type DeliverPlanResult struct {
	Status     string
	ResultCode string
	Tasks      []TaskResult
}

// activityOptions bundles the two ActivityOptions profiles this workflow
// uses: opts for genuine infra faults (activity-level retry, max 3,
// backoff 2x — docs/PLAN.md Task 12 Step 6), and noRetry for
// deterministic failures (a fencing conflict, an admission/signature
// check, appending a transition) that retrying immediately cannot fix.
type activityOptions struct {
	retry   workflow.ActivityOptions
	noRetry workflow.ActivityOptions
}

func newActivityOptions() activityOptions {
	return activityOptions{
		retry: workflow.ActivityOptions{
			StartToCloseTimeout: defaultTaskTimeout,
			HeartbeatTimeout:    2 * heartbeatInterval,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    time.Second,
				BackoffCoefficient: 2.0,
				MaximumAttempts:    3,
			},
		},
		noRetry: workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		},
	}
}

// DeliverPlan is the durable workflow that is the only place sequencing,
// retries, and side effects for delivering one approved plan live
// (Constitution C2, C4). It loads the approved plan, then runs each task
// sequentially — single repo, no waves (those are PEC's job, later) —
// acquiring a fenced worktree, executing, validating, and recording
// evidence for each, and emits exactly one canonical state.Transition per
// terminal outcome for the whole workflow (transitions carry no per-task
// identity — the state model's Transition schema is workflow-scoped).
//
// Determinism: this function and everything it calls directly (not
// through workflow.ExecuteActivity) must never call time.Now, rand, or any
// other non-deterministic source — use workflow.Now(ctx) instead. See
// lint_test.go's TestNoTimeNowInWorkflowFiles.
func DeliverPlan(ctx workflow.Context, in DeliverPlanInput) (DeliverPlanResult, error) {
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	executorName := in.ExecutorName
	if executorName == "" {
		executorName = "fake"
	}
	opts := newActivityOptions()
	// transitionSeq disambiguates the AppendTransition idempotency key
	// (internal/kernel/idempotency.go) across multiple transitions that
	// share the same target Status — e.g. a WAITING/budget pause-resume
	// cycle produces a second `to=RUNNING` transition after the workflow's
	// very first PENDING->RUNNING one. Keying AppendTransition's TaskID on
	// `to` alone (as this package did before Task 29) silently drops the
	// second transition: withReceipt finds the first one's receipt already
	// recorded under that key and returns it unwritten. Deterministic
	// (workflow-code-local counter, no time/rand), so safe across replay.
	transitionSeq := 0
	nextTransitionSeq := func() int {
		transitionSeq++
		return transitionSeq
	}

	loadCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
	var loaded LoadApprovedPlanOutput
	loadErr := workflow.ExecuteActivity(loadCtx, ActivityLoadApprovedPlan, LoadApprovedPlanInput{
		PlanID:       in.PlanID,
		PlanFilePath: in.PlanFilePath,
	}).Get(loadCtx, &loaded)

	// The canonical lifecycle (state-model.md §1) has no PENDING->FAILED
	// edge — every workflow passes through RUNNING first, even one that
	// fails immediately on its very first activity. No task has completed
	// yet, so the checkpoint recorded here is always the empty one.
	appendTransition(ctx, opts.noRetry, workflowID, state.StatusPending, state.StatusRunning, "", recovery.Checkpoint{}, nextTransitionSeq())
	if loadErr != nil {
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusFailed, "admission-rejected", recovery.Checkpoint{}, nextTransitionSeq())
		return DeliverPlanResult{Status: string(state.StatusFailed), ResultCode: string(state.ResultAdmissionRejected)}, loadErr
	}

	results := make([]TaskResult, 0, len(loaded.Tasks))
	classification := ""

	// Task 56 (TX-03): optionally consult PEC for wave ordering.
	// The kernel validates the proposal against its own dependency check
	// before use; a malformed or untrustworthy proposal is silently ignored
	// and the sequential fallback is used (distrust principle).
	tasks := pecOrderedTasks(loaded.Tasks)

	// checkpoint tracks how far this workflow has durably progressed —
	// the last task to fully complete plus every evidence bundle recorded
	// up to and including it (docs/PLAN.md Task 16 Step 1) — so the
	// terminal transition below carries an operator-visible CheckpointID
	// regardless of which task the workflow stopped on.
	checkpoint := recovery.Checkpoint{}

taskLoop:
	for _, task := range tasks {
		if ctx.Err() != nil {
			classification = "cancelled"
			break taskLoop
		}

		var result TaskResult
		var evidenceID, taskClassification string
		for budgetAttempt := 1; ; budgetAttempt++ {
			result, evidenceID, taskClassification = runTask(ctx, opts, workflowID, in, task, executorName, budgetAttempt)
			if taskClassification != "budget" {
				break
			}

			// Constitution C19 / docs/PLAN.md Task 29: an exhausted budget
			// envelope pauses the workflow rather than failing it — WAITING
			// with the registered "budget" reason, resumable via
			// SignalBudgetRaised once an operator runs `foundry budget
			// raise` (Task 30's real notification engine doesn't exist yet;
			// this is the smallest reversible stub for the operator-visible
			// signal that a workflow is stuck on cost, not a code bug).
			workflow.GetLogger(ctx).Warn("kernel: budget exhausted, workflow waiting", "workflow_id", workflowID, "task_id", task.ID)
			appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusWaiting, state.ReasonBudget, checkpoint, nextTransitionSeq())
			workflow.GetSignalChannel(ctx, SignalBudgetRaised).Receive(ctx, nil)
			if ctx.Err() != nil {
				taskClassification = "cancelled"
				break
			}
			appendTransition(ctx, opts.noRetry, workflowID, state.StatusWaiting, state.StatusRunning, "", checkpoint, nextTransitionSeq())
		}

		results = append(results, result)
		if evidenceID != "" {
			checkpoint.EvidenceIDs = append(checkpoint.EvidenceIDs, evidenceID)
		}
		if taskClassification != "" {
			classification = taskClassification
			break taskLoop
		}
		checkpoint.LastCompletedTaskID = task.ID
	}

	switch classification {
	case "":
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusSucceeded, "", checkpoint, nextTransitionSeq())
		return DeliverPlanResult{Status: string(state.StatusSucceeded), Tasks: results}, nil
	case "cancelled":
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		appendTransition(disconnected, opts.noRetry, workflowID, state.StatusRunning, state.StatusCancelled, "", checkpoint, nextTransitionSeq())
		return DeliverPlanResult{Status: string(state.StatusCancelled), Tasks: results}, ctx.Err()
	case "admission-rejected":
		// A mid-flight RecheckApproval failure (revoked or expired
		// ApprovedPlan) — same Reason/ResultCode pairing as the initial
		// LoadApprovedPlan admission failure above, so both admission
		// rejections (start-of-workflow and mid-flight) are
		// indistinguishable to a consumer of the result code.
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusFailed, state.Reason(classification), checkpoint, nextTransitionSeq())
		return DeliverPlanResult{Status: string(state.StatusFailed), ResultCode: string(state.ResultAdmissionRejected), Tasks: results}, nil
	default:
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusFailed, state.Reason(classification), checkpoint, nextTransitionSeq())
		return DeliverPlanResult{Status: string(state.StatusFailed), ResultCode: classification, Tasks: results}, nil
	}
}

// runTask runs one plan task end to end: RecheckApproval -> ReserveBudget ->
// AcquireLease -> AcquireWorktree -> ExecuteTask -> ValidateTask ->
// RecordEvidence -> ReleaseWorktree. A "budget" classification (ReserveBudget
// reports its envelope exhausted) is handled by DeliverPlan's caller, which
// pauses to WAITING and retries this same task once the envelope is raised,
// rather than treating it as a terminal failure. It
// returns the task's TaskResult, the evidence bundle ID RecordEvidence
// produced (empty if it never got that far), and, on failure, a non-empty
// failure-classification string (state-model.md §2's registry) explaining
// why the whole plan is about to terminate FAILED or CANCELLED. The
// worktree is released via defer so it happens exactly once, after
// RecordEvidence has had a chance to read the workspace, regardless of
// which step failed.
func runTask(ctx workflow.Context, opts activityOptions, workflowID string, in DeliverPlanInput, task plan.Task, executorName string, budgetAttempt int) (TaskResult, string, string) {
	failed := TaskResult{TaskID: task.ID, Failed: true}

	// Constitution C7 rule 5: re-check the approval before every task, not
	// just once at admission. This runs before any lease/worktree for this
	// task is acquired, so a halt here never leaves an orphaned worktree —
	// the previous task's worktree (if any) was already released by its own
	// runTask call.
	recheckCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
	var recheck RecheckApprovalOutput
	if err := workflow.ExecuteActivity(recheckCtx, ActivityRecheckApproval, RecheckApprovalInput{
		PlanID: in.PlanID,
	}).Get(recheckCtx, &recheck); err != nil {
		return failed, "", cancelOr(ctx, "admission-rejected")
	}

	// Constitution C19: budgets are enforced before spend, not after — this
	// runs before any lease/worktree/executor invocation for this task, so
	// an exhausted envelope never lets ExecuteTask (the only activity that
	// actually spends money) run at all.
	reserveCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
	var reserved ReserveBudgetOutput
	if err := workflow.ExecuteActivity(reserveCtx, ActivityReserveBudget, ReserveBudgetInput{
		WorkflowID:   workflowID,
		TaskID:       task.ID,
		ExecutorName: executorName,
		Attempt:      budgetAttempt,
	}).Get(reserveCtx, &reserved); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}
	if reserved.Exhausted {
		return failed, "", cancelOr(ctx, "budget")
	}

	leaseResource := "worktree:" + workflowID + ":" + task.ID
	leaseCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
	var lease AcquireLeaseOutput
	if err := workflow.ExecuteActivity(leaseCtx, ActivityAcquireLease, AcquireLeaseInput{
		Resource:   leaseResource,
		Holder:     workflowID,
		TTLSeconds: int(defaultLeaseTTL.Seconds()),
	}).Get(leaseCtx, &lease); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}

	acquireCtx := workflow.WithActivityOptions(ctx, opts.retry)
	var ws AcquireWorktreeOutput
	if err := workflow.ExecuteActivity(acquireCtx, ActivityAcquireWorktree, AcquireWorktreeInput{
		WorkflowID:    workflowID,
		TaskID:        task.ID,
		Attempt:       1,
		RepoPath:      in.RepoPath,
		LeaseResource: leaseResource,
		LeaseToken:    lease.Token,
	}).Get(acquireCtx, &ws); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}
	defer func() {
		releaseCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
		_ = workflow.ExecuteActivity(releaseCtx, ActivityReleaseWorktree, ReleaseWorktreeInput{
			WorkflowID: workflowID,
			TaskID:     task.ID,
		}).Get(releaseCtx, nil)
	}()

	execCtx := workflow.WithActivityOptions(ctx, opts.retry)
	var execOut ExecuteTaskOutput
	if err := workflow.ExecuteActivity(execCtx, ActivityExecuteTask, ExecuteTaskInput{
		WorkflowID:        workflowID,
		TaskID:            task.ID,
		Attempt:           1,
		ExecutorName:      executorName,
		ExplicitExecutor:  task.Executor,
		TaskClass:         task.Class,
		ExecutorAllowlist: in.ExecutorAllowlist,
		WorkspacePath:     ws.Path,
		Packet: executor.TaskPacket{
			PlanID:             in.PlanID,
			TaskID:             task.ID,
			Goal:               task.Goal,
			Commands:           task.Commands,
			ValidationCommands: task.ValidationCommands,
			TimeoutSec:         int(defaultTaskTimeout.Seconds()),
			Class:              task.Class,
			PhaseHint:          phaseHintFor(task),
		},
	}).Get(execCtx, &execOut); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}

	// Task 120 (COST-02): incur the task's real cost against its reservation on
	// completion. Cost accounting must never fail the plan — a recording error
	// is logged by the activity and the plan proceeds; the reservation stands.
	if reserved.EntryID != "" {
		recordCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
		var recorded RecordCostOutput
		_ = workflow.ExecuteActivity(recordCtx, ActivityRecordCost, RecordCostInput{
			WorkflowID:   workflowID,
			TaskID:       task.ID,
			Attempt:      budgetAttempt,
			EntryID:      reserved.EntryID,
			ExecutorName: execOut.ExecutorUsed,
			Usage:        execOut.Usage,
		}).Get(recordCtx, &recorded)
	}

	validateCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
	var validated ValidateTaskOutput
	if err := workflow.ExecuteActivity(validateCtx, ActivityValidateTask, ValidateTaskInput{
		ExecuteFailed:      execOut.Failed,
		WorkspacePath:      ws.Path,
		ValidationCommands: task.ValidationCommands,
		Attempt:            budgetAttempt,
		PreClassification:  execOut.Classification,
	}).Get(validateCtx, &validated); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}

	evidenceCtx := workflow.WithActivityOptions(ctx, opts.retry)
	var evidenced RecordEvidenceOutput
	if err := workflow.ExecuteActivity(evidenceCtx, ActivityRecordEvidence, RecordEvidenceInput{
		WorkflowID:    workflowID,
		TaskID:        task.ID,
		Attempt:       1,
		WorkspacePath: ws.Path,
		ArtifactPaths: execOut.ArtifactPaths,
		ExecuteFailed: execOut.Failed,
		ExecutorUsed:  execOut.ExecutorUsed,
	}).Get(evidenceCtx, &evidenced); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}

	if !validated.Validated {
		// validated.Reason is ValidateTask's real verify.Classification
		// (docs/PLAN.md Task 99/SKP-11R) — passed through as-is, not
		// hardcoded to "verification-failed" regardless of what actually
		// failed (a policy-violation or no-progress classification must
		// surface as itself, not be relabeled). Safe against
		// state.Transition.Validate: FAILED constrains only a set
		// ResultCode, never Reason (internal/state/transition.go).
		return failed, evidenced.BundleID, validated.Reason
	}
	return TaskResult{TaskID: task.ID, Failed: false}, evidenced.BundleID, ""
}

// cancelOr returns "cancelled" if ctx has been canceled, otherwise
// fallback — used so an activity error observed after the workflow's own
// context was canceled is reported as a cancellation, not a spurious
// environment failure.
func cancelOr(ctx workflow.Context, fallback string) string {
	if ctx.Err() != nil {
		return "cancelled"
	}
	return fallback
}

// appendTransition builds and validates a canonical state.Transition for
// the from->to status change, then durably persists it via the
// AppendTransition activity. It never itself performs the persistence —
// only ExecuteActivity does — keeping workflow.go free of side effects.
// A validation failure here indicates a bug in this package (an illegal
// from->to pair or invariant violation): it is logged and the transition
// is not sent, rather than corrupting the durable stream with an invalid
// record.
//
// seq is a caller-assigned, monotonically increasing call index
// (DeliverPlan's nextTransitionSeq) folded into the AppendTransition
// idempotency key alongside `to`. Docs/PLAN.md Task 29's WAITING/budget
// pause-resume cycle can produce two transitions with the same `to`
// Status within one workflow (e.g. two separate `to=RUNNING` transitions:
// the initial PENDING->RUNNING and a later WAITING->RUNNING resume) — `to`
// alone is no longer a unique key, so seq disambiguates them; without it,
// withReceipt's idempotency cache would silently drop the second write.
func appendTransition(ctx workflow.Context, opts workflow.ActivityOptions, workflowID string, from, to state.Status, reason state.Reason, checkpoint recovery.Checkpoint, seq int) {
	t := state.Transition{
		WorkflowID:   workflowID,
		Status:       to,
		Reason:       reason,
		CheckpointID: checkpoint.ID(),
		OccurredAt:   workflow.Now(ctx),
	}
	if err := t.Validate(from, to); err != nil {
		workflow.GetLogger(ctx).Error("kernel: invalid transition", "error", err, "from", from, "to", to)
		return
	}

	actCtx := workflow.WithActivityOptions(ctx, opts)
	if err := workflow.ExecuteActivity(actCtx, ActivityAppendTransition, AppendTransitionInput{
		WorkflowID: workflowID,
		TaskID:     fmt.Sprintf("workflow:%d:%s", seq, to),
		Attempt:    1,
		Transition: t,
	}).Get(actCtx, nil); err != nil {
		workflow.GetLogger(ctx).Error("kernel: append transition failed", "error", err, "to", to)
	}
}

// pecOrderedTasks optionally consults PEC for wave-ordered task execution.
// The kernel is the final authority: if PEC's proposal is malformed (unknown
// task IDs or a cycle), the sequential fallback is used. This satisfies
// Task 56's distrust requirement: "malformed proposal ignored, kernel falls
// back to sequential" (docs/PLAN.md Task 56 / TX-03 Steps).
func pecOrderedTasks(tasks []plan.Task) []plan.Task {
	if len(tasks) == 0 {
		return tasks
	}
	doc := plan.Document{Tasks: tasks}
	proposal, err := pec.ProposeWaves(doc)
	if err != nil {
		// Cycle or unknown task: distrust, fall back to sequential.
		return tasks
	}
	if err := pec.ValidateWaveProposal(proposal, doc); err != nil {
		// Malformed proposal: distrust, fall back to sequential.
		return tasks
	}
	// Flatten waves into sequential task list (waves[0] first, then waves[1], etc.).
	// Within each wave, tasks are sorted by ID (PEC's deterministic tie-break).
	// Current DeliverPlan runs tasks sequentially so flattening is correct;
	// true concurrent wave dispatch is a future enhancement.
	ordered := make([]plan.Task, 0, len(tasks))
	taskByID := make(map[string]plan.Task, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID] = t
	}
	for _, wave := range proposal.Waves {
		for _, id := range wave {
			if t, ok := taskByID[id]; ok {
				ordered = append(ordered, t)
			}
		}
	}
	if len(ordered) != len(tasks) {
		// Proposal didn't cover all tasks: distrust, fall back to sequential.
		return tasks
	}
	return ordered
}
