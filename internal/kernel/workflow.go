package kernel

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
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
	ActivityAcquireLease     = "AcquireLease"
	ActivityAcquireWorktree  = "AcquireWorktree"
	ActivityReleaseWorktree  = "ReleaseWorktree"
	ActivityExecuteTask      = "ExecuteTask"
	ActivityValidateTask     = "ValidateTask"
	ActivityRecordEvidence   = "RecordEvidence"
	ActivityAppendTransition = "AppendTransition"
)

// defaultTaskTimeout bounds a single task's ExecuteTask activity. plan.Task
// (Task 6's schema) carries no per-task timeout field, so this package
// applies one fixed default for every task rather than inventing a new
// plan-schema field out of scope for this task (decision: smallest
// reversible option per the no-gaps rule; a future task can add a
// per-task TimeoutSec to plan.Task without changing this package's
// behavior for plans that don't set it).
const defaultTaskTimeout = 10 * time.Minute

// defaultLeaseTTL bounds how long a worktree fencing token is valid before
// it can be reclaimed by a new holder.
const defaultLeaseTTL = 15 * time.Minute

// DeliverPlanInput is DeliverPlan's workflow input.
type DeliverPlanInput struct {
	PlanID       string
	PlanFilePath string
	RepoPath     string
	ExecutorName string
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
	appendTransition(ctx, opts.noRetry, workflowID, state.StatusPending, state.StatusRunning, "", recovery.Checkpoint{})
	if loadErr != nil {
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusFailed, "admission-rejected", recovery.Checkpoint{})
		return DeliverPlanResult{Status: string(state.StatusFailed), ResultCode: string(state.ResultAdmissionRejected)}, loadErr
	}

	results := make([]TaskResult, 0, len(loaded.Tasks))
	classification := ""
	// checkpoint tracks how far this workflow has durably progressed —
	// the last task to fully complete plus every evidence bundle recorded
	// up to and including it (docs/PLAN.md Task 16 Step 1) — so the
	// terminal transition below carries an operator-visible CheckpointID
	// regardless of which task the workflow stopped on.
	checkpoint := recovery.Checkpoint{}

taskLoop:
	for _, task := range loaded.Tasks {
		if ctx.Err() != nil {
			classification = "cancelled"
			break taskLoop
		}

		result, evidenceID, taskClassification := runTask(ctx, opts, workflowID, in, task, executorName)
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
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusSucceeded, "", checkpoint)
		return DeliverPlanResult{Status: string(state.StatusSucceeded), Tasks: results}, nil
	case "cancelled":
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		appendTransition(disconnected, opts.noRetry, workflowID, state.StatusRunning, state.StatusCancelled, "", checkpoint)
		return DeliverPlanResult{Status: string(state.StatusCancelled), Tasks: results}, ctx.Err()
	default:
		appendTransition(ctx, opts.noRetry, workflowID, state.StatusRunning, state.StatusFailed, state.Reason(classification), checkpoint)
		return DeliverPlanResult{Status: string(state.StatusFailed), ResultCode: classification, Tasks: results}, nil
	}
}

// runTask runs one plan task end to end: AcquireLease -> AcquireWorktree ->
// ExecuteTask -> ValidateTask -> RecordEvidence -> ReleaseWorktree. It
// returns the task's TaskResult, the evidence bundle ID RecordEvidence
// produced (empty if it never got that far), and, on failure, a non-empty
// failure-classification string (state-model.md §2's registry) explaining
// why the whole plan is about to terminate FAILED or CANCELLED. The
// worktree is released via defer so it happens exactly once, after
// RecordEvidence has had a chance to read the workspace, regardless of
// which step failed.
func runTask(ctx workflow.Context, opts activityOptions, workflowID string, in DeliverPlanInput, task plan.Task, executorName string) (TaskResult, string, string) {
	failed := TaskResult{TaskID: task.ID, Failed: true}

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
		WorkflowID:    workflowID,
		TaskID:        task.ID,
		Attempt:       1,
		ExecutorName:  executorName,
		WorkspacePath: ws.Path,
		Packet: executor.TaskPacket{
			PlanID:             in.PlanID,
			TaskID:             task.ID,
			Goal:               task.Goal,
			Commands:           task.Commands,
			ValidationCommands: task.ValidationCommands,
			TimeoutSec:         int(defaultTaskTimeout.Seconds()),
		},
	}).Get(execCtx, &execOut); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}

	validateCtx := workflow.WithActivityOptions(ctx, opts.noRetry)
	var validated ValidateTaskOutput
	if err := workflow.ExecuteActivity(validateCtx, ActivityValidateTask, ValidateTaskInput{
		ExecuteFailed: execOut.Failed,
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
	}).Get(evidenceCtx, &evidenced); err != nil {
		return failed, "", cancelOr(ctx, "environment")
	}

	if !validated.Validated {
		return failed, evidenced.BundleID, "verification-failed"
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
func appendTransition(ctx workflow.Context, opts workflow.ActivityOptions, workflowID string, from, to state.Status, reason state.Reason, checkpoint recovery.Checkpoint) {
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
		TaskID:     fmt.Sprintf("workflow:%s", to),
		Attempt:    1,
		Transition: t,
	}).Get(actCtx, nil); err != nil {
		workflow.GetLogger(ctx).Error("kernel: append transition failed", "error", err, "to", to)
	}
}
