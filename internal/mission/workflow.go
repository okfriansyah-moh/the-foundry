package mission

import (
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// defaultIterationsBeforeContinueAsNew bounds MissionLoop's Temporal history
// growth: after this many loop iterations the workflow continues-as-new
// carrying its loop state (docs/PLAN.md Task 106).
const defaultIterationsBeforeContinueAsNew = 1000

// Activity names, registered by cmd/foundryd/main.go and referenced here by
// name so workflow.go never imports the Activities struct itself (mirrors
// internal/kernel/workflow.go's own separation of workflow code from
// activity implementations).
const (
	ActivityRequireLoopContract     = "MissionRequireLoopContract"
	ActivityRequireReadiness        = "MissionRequireReadiness"
	ActivityObserveLedger           = "MissionObserveLedger"
	ActivityCheckBudget             = "MissionCheckBudget"
	ActivityAppendMissionTransition = "MissionAppendTransition"
	ActivityRecordMissionState      = "MissionRecordState"
	ActivityRecordGateEvent         = "MissionRecordGateEvent"
	ActivityResolveGateEvent        = "MissionResolveGateEvent"
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
	// which the ceremony/spec-synthesis/plan-generation pipeline (Tasks
	// 41–44) queues one product delivery cycle. MissionLoop's own job stops
	// at orchestrating this call — it makes no decision about what to
	// deliver or whether to.
	SignalTriggerDelivery = "mission-trigger-delivery"
	// SignalManualPause is `foundry mission pause`'s signal: an operator-
	// initiated pause independent of any automatic pause_when trigger.
	// MissionLoop treats it exactly like an automatic pause -- WAITING
	// with the registered "human-command" reason -- resumable via
	// SignalResumeMission or terminable via SignalKillMission, same as any
	// other WAITING state.
	SignalManualPause = "mission-manual-pause"
	// SignalEnterHumanGate escalates an unforeseen human action while a
	// mission is running; MissionLoop must pause WAITING/unforeseen-human-gate
	// and carry the exact action through its gate event record.
	SignalEnterHumanGate = "mission-enter-human-gate"
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

// HumanGateRequest is SignalEnterHumanGate's payload.
type HumanGateRequest struct {
	RequestedBy string
	Action      string
}

// MissionLoopInput is MissionLoop's workflow input.
type MissionLoopInput struct {
	MissionID string
	Contract  Contract
	// DeliveryTaskQueue is the explicit lane task queue child DeliverPlan
	// executions run on (docs/PLAN.md Task 106). Empty inherits the parent's
	// task queue.
	DeliveryTaskQueue string
	// The Carried* fields preserve loop state across ContinueAsNew so a
	// months-long mission runs with bounded Temporal history without losing
	// deliverySeq (child workflow IDs stay unique/deterministic) or the
	// evaluator's progress (docs/PLAN.md Task 106).
	CarriedDeliverySeq int
	CarriedEvalState   EvalState
	CarriedIteration   int
	// MaxIterationsBeforeContinue bounds history growth by continuing-as-new
	// after this many observe/evaluate cycles. 0 uses the package default.
	MaxIterationsBeforeContinue int
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
	if err := workflow.ExecuteActivity(reqCtx, ActivityRequireReadiness, in.MissionID).Get(reqCtx, nil); err != nil {
		return MissionLoopResult{}, fmt.Errorf("mission: MissionLoop refuses to start without readiness pass: %w", err)
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
	humanGateCh := workflow.GetSignalChannel(ctx, SignalEnterHumanGate)

	evalState := in.CarriedEvalState
	deliverySeq := in.CarriedDeliverySeq
	iteration := in.CarriedIteration
	maxIterations := in.MaxIterationsBeforeContinue
	if maxIterations <= 0 {
		maxIterations = defaultIterationsBeforeContinueAsNew
	}

	for {
		var killReq KillRequest
		killed := false
		var deliverIn kernel.DeliverPlanInput
		triggered := false
		var pauseReq PauseRequest
		pauseRequested := false
		var humanGateReq HumanGateRequest
		humanGateRequested := false

		// Bounded history: after a configurable number of observe/evaluate
		// cycles, continue-as-new carrying loop state so a months-long
		// mission never exhausts Temporal history (docs/PLAN.md Task 106).
		// deliverySeq is preserved so child workflow IDs stay unique.
		if iteration >= maxIterations {
			// Signals buffered on this run are NOT carried across
			// ContinueAsNew (Temporal drops them), so drain any pending signal
			// and handle it this iteration rather than losing it — an operator
			// `foundry mission kill` must never be dropped at the CAN boundary.
			switch {
			case killCh.ReceiveAsync(&killReq):
				killed = true
			case deliverCh.ReceiveAsync(&deliverIn):
				triggered = true
			case pauseCh.ReceiveAsync(&pauseReq):
				pauseRequested = true
			case humanGateCh.ReceiveAsync(&humanGateReq):
				humanGateRequested = true
			default:
				return MissionLoopResult{}, workflow.NewContinueAsNewError(ctx, MissionLoop, MissionLoopInput{
					MissionID:                   in.MissionID,
					Contract:                    in.Contract,
					DeliveryTaskQueue:           in.DeliveryTaskQueue,
					CarriedDeliverySeq:          deliverySeq,
					CarriedEvalState:            evalState,
					CarriedIteration:            0,
					MaxIterationsBeforeContinue: in.MaxIterationsBeforeContinue,
				})
			}
			// A drained signal falls through to the shared handling below.
		} else {
			iteration++

			timerCtx, cancelTimer := workflow.WithCancel(ctx)
			timer := workflow.NewTimer(timerCtx, interval)
			sel := workflow.NewSelector(ctx)

			sel.AddReceive(killCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &killReq)
				killed = true
			})
			sel.AddReceive(deliverCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &deliverIn)
				triggered = true
			})
			sel.AddReceive(pauseCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &pauseReq)
				pauseRequested = true
			})
			sel.AddReceive(humanGateCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &humanGateReq)
				humanGateRequested = true
			})
			sel.AddFuture(timer, func(workflow.Future) {})

			sel.Select(ctx)
			cancelTimer()
		}

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
		if humanGateRequested {
			killedWhilePaused, waitKillReq := EnterHumanGate(ctx, noRetryOpts, workflowID, in.MissionID, humanGateReq.Action, iteration, killCh, resumeCh)
			if killedWhilePaused {
				return finishKilled(ctx, noRetryOpts, workflowID, state.StatusWaiting, waitKillReq, evalState), nil
			}
			continue
		}

		if triggered {
			// (4) Validate the trigger payload before it reaches DeliverPlan —
			// an empty/malformed DeliverPlanInput is refused with a recorded
			// reason, never forwarded (docs/PLAN.md Task 106).
			if err := validateDeliverInput(deliverIn); err != nil {
				workflow.GetLogger(ctx).Warn("mission: refusing malformed delivery trigger", "error", err.Error())
				appendTransition(ctx, noRetryOpts, workflowID, state.StatusRunning, state.StatusRunning, state.ReasonHumanCommand, "")
				continue
			}

			deliverySeq++
			childOpts := workflow.ChildWorkflowOptions{
				WorkflowID:        fmt.Sprintf("%s-delivery-%d", workflowID, deliverySeq),
				ParentClosePolicy: enums.PARENT_CLOSE_POLICY_TERMINATE,
			}
			if in.DeliveryTaskQueue != "" {
				childOpts.TaskQueue = in.DeliveryTaskQueue
			}
			childCtx, cancelChild := workflow.WithCancel(workflow.WithChildOptions(ctx, childOpts))
			childFut := workflow.ExecuteChildWorkflow(childCtx, kernel.DeliverPlan, deliverIn)

			// (3) Kill during delivery: select over the child future AND
			// killCh, so `foundry mission kill` cancels the in-flight
			// DeliverPlan through the child's cancellation scope rather than
			// being ignored until the child finishes.
			childSel := workflow.NewSelector(ctx)
			var childResult kernel.DeliverPlanResult
			var childErr error
			childDone := false
			childSel.AddFuture(childFut, func(f workflow.Future) {
				childErr = f.Get(childCtx, &childResult)
				childDone = true
			})
			var deliverKillReq KillRequest
			killedDuringDelivery := false
			childSel.AddReceive(killCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &deliverKillReq)
				killedDuringDelivery = true
			})
			childSel.Select(ctx)

			if killedDuringDelivery {
				cancelChild()
				return finishKilled(ctx, noRetryOpts, workflowID, state.StatusRunning, deliverKillReq, evalState), nil
			}
			cancelChild()

			// (2) Record the real child outcome rather than swallowing it: a
			// failed delivery is distinguishable from a successful one and
			// feeds the next evaluator cycle through recorded mission state.
			recordDeliveryOutcome(ctx, retryOpts, in.MissionID, deliverySeq, childResult, childErr)
			if childDone && (childErr != nil || childResultFailed(childResult)) {
				appendTransition(ctx, noRetryOpts, workflowID, state.StatusRunning, state.StatusRunning, state.ReasonBlockedDependency, "")
			}
			continue
		}

		// Timer fired: run one observe/evaluate cycle.
		obsCtx := workflow.WithActivityOptions(ctx, retryOpts)
		var sample LedgerSample
		if err := workflow.ExecuteActivity(obsCtx, ActivityObserveLedger, observeLedgerInput{
			MissionID: in.MissionID,
			At:        workflow.Now(ctx),
		}).Get(obsCtx, &sample); err != nil {
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
		_ = workflow.ExecuteActivity(recCtx, ActivityRecordMissionState, MissionStateInput{
			WorkflowID:    workflowID,
			LoopIteration: iteration,
			MissionID:     in.MissionID,
			EvalState:     evalState,
			Sample:        sample,
			Outcome:       outcome,
			At:            workflow.Now(ctx),
		}).Get(recCtx, nil)

		switch {
		case outcome.Continue:
			continue
		case outcome.Status == state.StatusWaiting:
			if outcome.Reason == state.ReasonUnforeseenHumanGate {
				killedWhilePaused, waitKillReq := EnterHumanGate(ctx, noRetryOpts, workflowID, in.MissionID, PauseUnforeseenHumanGate, iteration, killCh, resumeCh)
				if killedWhilePaused {
					return finishKilled(ctx, noRetryOpts, workflowID, state.StatusWaiting, waitKillReq, evalState), nil
				}
				continue
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

// EnterHumanGate handles an unforeseen human gate round-trip:
// RUNNING->WAITING/unforeseen-human-gate with exact action recorded, then
// resumes on SignalResumeMission.
func EnterHumanGate(ctx workflow.Context, noRetryOpts workflow.ActivityOptions, workflowID, missionID, action string, iteration int, killCh, resumeCh workflow.ReceiveChannel) (bool, KillRequest) {
	if action == "" {
		action = PauseUnforeseenHumanGate
	}
	workflow.GetLogger(ctx).Warn("mission: unforeseen human gate", "workflow_id", workflowID, "action", action)
	recCtx := workflow.WithActivityOptions(ctx, noRetryOpts)
	// Deterministic gate id from (workflowID, iteration): a retried escalation
	// derives the same id and addresses the same gate_events row instead of
	// orphaning one, and the resolve below closes exactly it (Task 122).
	gateID := deterministicID("gate", workflowID, iteration)
	if err := workflow.ExecuteActivity(recCtx, ActivityRecordGateEvent, GateEventInput{
		WorkflowID:    workflowID,
		LoopIteration: iteration,
		GateEventID:   gateID,
		MissionID:     missionID,
		Action:        action,
		OccurredAt:    workflow.Now(ctx),
	}).Get(recCtx, &gateID); err != nil {
		workflow.GetLogger(ctx).Error("mission: failed to record gate event", "workflow_id", workflowID, "error", err)
	}
	appendTransition(ctx, noRetryOpts, workflowID, state.StatusRunning, state.StatusWaiting, state.ReasonUnforeseenHumanGate, "")
	killed, killReq := pauseAndWait(ctx, noRetryOpts, workflowID, state.ReasonUnforeseenHumanGate, killCh, resumeCh)
	if killed {
		return true, killReq
	}
	if gateID != "" {
		resolveCtx := workflow.WithActivityOptions(ctx, noRetryOpts)
		if err := workflow.ExecuteActivity(resolveCtx, ActivityResolveGateEvent, ResolveGateInput{
			WorkflowID:    workflowID,
			LoopIteration: iteration,
			GateEventID:   gateID,
			Resolution:    "resumed via mission-resume signal",
			ResolvedAt:    workflow.Now(ctx),
		}).Get(resolveCtx, nil); err != nil {
			workflow.GetLogger(ctx).Error("mission: failed to resolve gate event", "gate_id", gateID, "error", err)
		}
	}
	return false, KillRequest{}
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

// MissionStateInput is RecordMissionState's activity input. The
// {WorkflowID, LoopIteration, Attempt} triple lets the activity build a
// receipt key and a deterministic row id (docs/PLAN.md Task 122).
type MissionStateInput struct {
	WorkflowID    string
	LoopIteration int
	Attempt       int
	MissionID     string
	EvalState     EvalState
	Sample        LedgerSample
	Outcome       Outcome
	At            time.Time
}

// observeLedgerInput is ObserveLedger's activity input. At is passed from
// workflow.Now(ctx) so a retried observation samples the same instant.
type observeLedgerInput struct {
	MissionID string
	At        time.Time
}

type GateEventInput struct {
	WorkflowID    string
	LoopIteration int
	Attempt       int
	GateEventID   string
	MissionID     string
	Action        string
	OccurredAt    time.Time
}

type ResolveGateInput struct {
	WorkflowID    string
	LoopIteration int
	Attempt       int
	GateEventID   string
	Resolution    string
	ResolvedAt    time.Time
}

// validateDeliverInput rejects an empty or malformed DeliverPlanInput before
// it reaches DeliverPlan (docs/PLAN.md Task 106): a mission never forwards a
// trigger payload it cannot vouch for.
func validateDeliverInput(in kernel.DeliverPlanInput) error {
	if in.PlanID == "" {
		return fmt.Errorf("mission: delivery trigger has empty plan id")
	}
	if in.PlanFilePath == "" {
		return fmt.Errorf("mission: delivery trigger has empty plan file path")
	}
	if in.RepoPath == "" {
		return fmt.Errorf("mission: delivery trigger has empty repo path")
	}
	return nil
}

// childResultFailed reports whether a child DeliverPlan terminated FAILED.
func childResultFailed(r kernel.DeliverPlanResult) bool {
	return r.Status == string(state.StatusFailed)
}

// recordDeliveryOutcome records a child delivery's real outcome so a failed
// delivery is never silently swallowed (docs/PLAN.md Task 106). It logs
// deterministically; the caller also appends a mission transition on failure.
func recordDeliveryOutcome(ctx workflow.Context, _ workflow.ActivityOptions, missionID string, seq int, r kernel.DeliverPlanResult, childErr error) {
	logger := workflow.GetLogger(ctx)
	switch {
	case childErr != nil:
		logger.Warn("mission: child delivery errored", "mission_id", missionID, "delivery_seq", seq, "error", childErr.Error())
	case childResultFailed(r):
		logger.Warn("mission: child delivery FAILED", "mission_id", missionID, "delivery_seq", seq, "result_code", r.ResultCode)
	default:
		logger.Info("mission: child delivery completed", "mission_id", missionID, "delivery_seq", seq, "status", r.Status)
	}
}
