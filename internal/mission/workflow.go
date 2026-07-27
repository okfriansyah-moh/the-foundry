package mission

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// Activity names, registered by cmd/foundryd/main.go and referenced here by
// name so workflow.go never imports the Activities struct itself (mirrors
// internal/kernel/workflow.go's own separation of workflow code from
// activity implementations).
const (
	ActivityRequireLoopContract     = "MissionRequireLoopContract"
	ActivityObserveLedger           = "MissionObserveLedger"
	ActivityCheckBudget             = "MissionCheckBudget"
	ActivityAppendMissionTransition = "MissionAppendTransition"
	ActivityRecordMissionState      = "MissionRecordState"
	ActivityRecordGateEvent         = "MissionRecordGateEvent"
)

// Signal names MissionLoop listens on.
const (
	// SignalKillMission carries a KillRequest; MissionLoop stops cleanly
	// with CANCELLED/MISSION_KILLED, whether it is currently observing or
	// paused WAITING.
	SignalKillMission = "mission-kill"
	// SignalResumeMission resumes a mission paused WAITING (an operator
	// raised the exhausted budget envelope, or resolved an unforeseen
	// human gate) -- mirrors internal/kernel.SignalBudgetRaised's role for
	// DeliverPlan.
	SignalResumeMission = "mission-resume"
	// SignalTriggerDelivery carries a kernel.DeliverPlanInput: the seam by
	// which a future task (Tasks 41-44's ceremony/spec-synthesis/plan-
	// generation pipeline, none of which exist yet) queues one product
	// delivery cycle. MissionLoop's own job stops at orchestrating this
	// call -- it makes no decision about what to deliver or whether to.
	SignalTriggerDelivery = "mission-trigger-delivery"
	// SignalManualPause is `foundry mission pause`'s signal: an operator-
	// initiated pause independent of any automatic pause_when trigger.
	// MissionLoop treats it exactly like an automatic pause -- WAITING
	// with the registered "human-command" reason -- resumable via
	// SignalResumeMission or terminable via SignalKillMission, same as any
	// other WAITING state.
	SignalManualPause = "mission-manual-pause"
)

// loopName is the loop_contracts.loop_name a mission's MissionLoop
// registers/checks under (mission-contract.md §3's universal loop
// contract).
func loopName(missionID string) string { return "mission:" + missionID }

// KillRequest is SignalKillMission's payload.
type KillRequest struct {
	RequestedBy string
	Reason      string
}

// PauseRequest is SignalManualPause's payload.
type PauseRequest struct {
	RequestedBy string
	Reason      string
}

// MissionLoopInput is MissionLoop's workflow input.
type MissionLoopInput struct {
	MissionID string
	Contract  Contract
}

// MissionLoopResult is MissionLoop's terminal workflow result.
type MissionLoopResult struct {
	Status      string
	ResultCode  string
	HandoffNote string
}

func newActivityOptions() (retryOpts, noRetryOpts workflow.ActivityOptions) {
	noRetryOpts = workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	retryOpts = workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	}
	return retryOpts, noRetryOpts
}

// MissionLoop is the cron-cadenced Temporal workflow driving one mission's
// observe/evaluate/pause/terminate lifecycle (Constitution C18: missions
// are formal, bounded contracts, never open loops). It only orchestrates:
// product delivery still goes through internal/kernel's own DeliverPlan
// workflow, invoked here as a child workflow on SignalTriggerDelivery,
// never duplicated (docs/PLAN.md Task 40 Scope).
//
// mission-contract.md §3: every loop MUST register a loop contract before
// it may run. The very first thing this function does is enforce that --
// via ActivityRequireLoopContract -- refusing to proceed at all when none
// is registered. cmd/fitlint's "missionloop" check statically proves this
// call is present in this function's body, so the refusal cannot be
// silently deleted without failing `make fitness`.
//
// Determinism: as with internal/kernel.DeliverPlan, this function and
// everything it calls directly (not through workflow.ExecuteActivity) must
// never call time.Now, rand, or any other non-deterministic source -- use
// workflow.Now(ctx) instead.
//
// decision (no-gaps rule): unlike internal/kernel's Activities, this
// package's mission-scoped activities (AppendMissionTransition,
// RecordMissionState, RecordGateEvent) are not wrapped in an idempotency-
// receipt layer the way kernel/idempotency.go's withReceipt wraps
// DeliverPlan's activities. Building that machinery (a lease-fenced
// receipt store keyed on workflow/task/attempt) for this task would
// duplicate a large piece of kernel's own infrastructure; the smallest
// reversible v1 accepts a narrow, known gap instead: a worker crash after
// one of these activities' Postgres write commits but before Temporal
// records the activity's success can, on retry, produce a duplicate
// audit-trail row (an extra mission_state/workflow_transitions record),
// never a duplicate side effect with authority consequences (no money
// moves, no SCM write, no second budget reservation happens here). This is
// called out explicitly rather than silently shipped as fully solved; a
// follow-up task can extend kernel's existing idempotency layer to cover
// this package if the gap proves material in practice.
func MissionLoop(ctx workflow.Context, in MissionLoopInput) (MissionLoopResult, error) {
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	retryOpts, noRetryOpts := newActivityOptions()

	reqCtx := workflow.WithActivityOptions(ctx, noRetryOpts)
	if err := workflow.ExecuteActivity(reqCtx, ActivityRequireLoopContract, in.MissionID).Get(reqCtx, nil); err != nil {
		return MissionLoopResult{}, fmt.Errorf("mission: MissionLoop refuses to start without a registered loop contract: %w", err)
	}

	interval, err := parseCadence(in.Contract.Cadence.Observe)
	if err != nil {
		return MissionLoopResult{}, fmt.Errorf("mission: invalid cadence.observe %q: %w", in.Contract.Cadence.Observe, err)
	}

	appendTransition(ctx, noRetryOpts, workflowID, state.StatusPending, state.StatusRunning, "", "")

	killCh := workflow.GetSignalChannel(ctx, SignalKillMission)
	deliverCh := workflow.GetSignalChannel(ctx, SignalTriggerDelivery)
	resumeCh := workflow.GetSignalChannel(ctx, SignalResumeMission)
	pauseCh := workflow.GetSignalChannel(ctx, SignalManualPause)

	evalState := EvalState{}
	deliverySeq := 0

	for {
		timerCtx, cancelTimer := workflow.WithCancel(ctx)
		timer := workflow.NewTimer(timerCtx, interval)
		sel := workflow.NewSelector(ctx)

		var killReq KillRequest
		killed := false
		sel.AddReceive(killCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &killReq)
			killed = true
		})

		var deliverIn kernel.DeliverPlanInput
		triggered := false
		sel.AddReceive(deliverCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &deliverIn)
			triggered = true
		})

		var pauseReq PauseRequest
		pauseRequested := false
		sel.AddReceive(pauseCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &pauseReq)
			pauseRequested = true
		})

		sel.AddFuture(timer, func(workflow.Future) {})

		sel.Select(ctx)
		cancelTimer()

		if killed {
			return finishKilled(ctx, noRetryOpts, workflowID, state.StatusRunning, killReq, evalState), nil
		}

		if pauseRequested {
			workflow.GetLogger(ctx).Warn("mission: manual pause requested", "requested_by", pauseReq.RequestedBy, "reason", pauseReq.Reason)
			appendTransition(ctx, noRetryOpts, workflowID, state.StatusRunning, state.StatusWaiting, state.ReasonHumanCommand, "")
			killedWhilePaused, waitKillReq := pauseAndWait(ctx, noRetryOpts, workflowID, state.ReasonHumanCommand, killCh, resumeCh)
			if killedWhilePaused {
				return finishKilled(ctx, noRetryOpts, workflowID, state.StatusWaiting, waitKillReq, evalState), nil
			}
			continue
		}

		if triggered {
			deliverySeq++
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID: fmt.Sprintf("%s-delivery-%d", workflowID, deliverySeq),
			})
			var childResult kernel.DeliverPlanResult
			// MissionLoop orchestrates the call only -- DeliverPlan's own
			// sequencing/retries/side effects are unchanged and untouched
			// here (Constitution C4). Its outcome feeds into the mission's
			// progress only via the next ledger observation, never
			// decided by this loop directly.
			_ = workflow.ExecuteChildWorkflow(childCtx, kernel.DeliverPlan, deliverIn).Get(childCtx, &childResult)
			continue
		}

		// Timer fired: run one observe/evaluate cycle.
		obsCtx := workflow.WithActivityOptions(ctx, retryOpts)
		var sample LedgerSample
		if err := workflow.ExecuteActivity(obsCtx, ActivityObserveLedger, in.MissionID).Get(obsCtx, &sample); err != nil {
			return MissionLoopResult{}, fmt.Errorf("mission: observe ledger: %w", err)
		}

		budgetCtx := workflow.WithActivityOptions(ctx, retryOpts)
		var sig Signal
		if err := workflow.ExecuteActivity(budgetCtx, ActivityCheckBudget, in.MissionID).Get(budgetCtx, &sig); err != nil {
			return MissionLoopResult{}, fmt.Errorf("mission: check budget: %w", err)
		}

		outcome, next := Evaluate(in.Contract, evalState, sample, sig)
		evalState = next

		recCtx := workflow.WithActivityOptions(ctx, retryOpts)
		_ = workflow.ExecuteActivity(recCtx, ActivityRecordMissionState, missionStateInput{
			MissionID: in.MissionID,
			EvalState: evalState,
			Sample:    sample,
			Outcome:   outcome,
			At:        workflow.Now(ctx),
		}).Get(recCtx, nil)

		switch {
		case outcome.Continue:
			continue
		case outcome.Status == state.StatusWaiting:
			if outcome.Reason == state.ReasonUnforeseenHumanGate {
				gateCtx := workflow.WithActivityOptions(ctx, retryOpts)
				_ = workflow.ExecuteActivity(gateCtx, ActivityRecordGateEvent, in.MissionID).Get(gateCtx, nil)
			}
			appendTransition(ctx, noRetryOpts, workflowID, state.StatusRunning, state.StatusWaiting, outcome.Reason, "")

			waitKilled, waitKillReq := pauseAndWait(ctx, noRetryOpts, workflowID, outcome.Reason, killCh, resumeCh)
			if waitKilled {
				return finishKilled(ctx, noRetryOpts, workflowID, state.StatusWaiting, waitKillReq, evalState), nil
			}
			continue
		default:
			appendTransition(ctx, noRetryOpts, workflowID, state.StatusRunning, outcome.Status, "", outcome.ResultCode)
			return MissionLoopResult{Status: string(outcome.Status), ResultCode: string(outcome.ResultCode)}, nil
		}
	}
}

// finishKilled builds the CANCELLED/MISSION_KILLED terminal result for a
// mission killed mid-loop -- from either RUNNING (observing) or WAITING
// (paused) -- with a clean product-state handoff note (docs/PLAN.md Task
// 40 Acceptance) summarizing the mission's progress at the moment of the
// kill so an operator picking this up has context without re-deriving it
// from mission_state rows.
func finishKilled(ctx workflow.Context, noRetryOpts workflow.ActivityOptions, workflowID string, from state.Status, req KillRequest, evalState EvalState) MissionLoopResult {
	note := fmt.Sprintf(
		"mission killed by %q (%s) after %d evaluation cycle(s), best net MRR observed $%.2f -- handing off current product state as-is; no further mission-driven changes will occur",
		req.RequestedBy, req.Reason, evalState.Cycles, evalState.BestNetMRRUSD,
	)
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	appendTransition(disconnected, noRetryOpts, workflowID, from, state.StatusCancelled, "", state.ResultMissionKilled)
	return MissionLoopResult{
		Status:      string(state.StatusCancelled),
		ResultCode:  string(state.ResultMissionKilled),
		HandoffNote: note,
	}
}

// pauseAndWait blocks (once the caller has already appended the
// RUNNING->WAITING transition for reason) until either an operator resumes
// the mission (SignalResumeMission) or kills it (SignalKillMission) while
// it is WAITING -- shared by both the evaluator-driven pause path and
// `foundry mission pause`'s manual-pause path, so the resume/kill-while-
// paused behavior is defined exactly once. On resume it appends the
// WAITING->RUNNING transition itself; on a kill, it returns immediately
// without appending anything further -- the caller's finishKilled call
// appends the terminal WAITING->CANCELLED transition.
func pauseAndWait(ctx workflow.Context, noRetryOpts workflow.ActivityOptions, workflowID string, reason state.Reason, killCh, resumeCh workflow.ReceiveChannel) (killed bool, killReq KillRequest) {
	workflow.GetLogger(ctx).Warn("mission: paused", "workflow_id", workflowID, "reason", reason)

	waitSel := workflow.NewSelector(ctx)
	waitSel.AddReceive(resumeCh, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
	waitSel.AddReceive(killCh, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, &killReq)
		killed = true
	})
	waitSel.Select(ctx)

	if killed {
		return true, killReq
	}
	appendTransition(ctx, noRetryOpts, workflowID, state.StatusWaiting, state.StatusRunning, "", "")
	return false, KillRequest{}
}

// AppendTransitionInput is AppendMissionTransition's input.
type AppendTransitionInput struct {
	WorkflowID string
	Transition state.Transition
}

// appendTransition builds and validates a canonical state.Transition for
// the from->to status change, then durably persists it via the
// AppendMissionTransition activity -- mirrors
// internal/kernel/workflow.go's own appendTransition helper exactly,
// scoped to this package's activity name and Store.
func appendTransition(ctx workflow.Context, opts workflow.ActivityOptions, workflowID string, from, to state.Status, reason state.Reason, resultCode state.ResultCode) {
	t := state.Transition{
		WorkflowID: workflowID,
		Status:     to,
		Reason:     reason,
		ResultCode: resultCode,
		OccurredAt: workflow.Now(ctx),
	}
	if err := t.Validate(from, to); err != nil {
		workflow.GetLogger(ctx).Error("mission: invalid transition", "error", err, "from", from, "to", to)
		return
	}

	actCtx := workflow.WithActivityOptions(ctx, opts)
	if err := workflow.ExecuteActivity(actCtx, ActivityAppendMissionTransition, AppendTransitionInput{WorkflowID: workflowID, Transition: t}).Get(actCtx, nil); err != nil {
		workflow.GetLogger(ctx).Error("mission: append transition failed", "error", err, "to", to)
	}
}

// missionStateInput is RecordMissionState's activity input.
type missionStateInput struct {
	MissionID string
	EvalState EvalState
	Sample    LedgerSample
	Outcome   Outcome
	At        time.Time
}
