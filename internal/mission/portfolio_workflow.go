package mission

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// PortfolioTaskQueue is the dedicated Temporal task queue the portfolio
// supervisor runs on -- "its own lane" (docs/PLAN.md Task 121). Keeping it off
// the delivery/mission lanes means portfolio supervision cannot starve, or be
// starved by, product delivery.
const PortfolioTaskQueue = "foundry-portfolio"

// PortfolioWorkflowID is the deterministic workflow ID a portfolio supervisor
// runs under, so a double start (a restart racing a manual start) collapses to
// one supervisor rather than two both racing the cap.
func PortfolioWorkflowID(portfolioID string) string { return "portfolio-" + portfolioID }

// Activity names for the portfolio supervisor, registered by
// cmd/foundryd/main.go and referenced here by name so the workflow never
// imports the activity struct (mirrors MissionLoop's own separation).
const (
	ActivityPortfolioReconcile = "PortfolioReconcile"
)

// defaultPortfolioIterations bounds PortfolioLoop's Temporal history growth
// before it continues-as-new, mirroring MissionLoop's own bound.
const defaultPortfolioIterations = 500

// PortfolioLoopInput is PortfolioLoop's workflow input.
type PortfolioLoopInput struct {
	PortfolioID string
	// MissionTaskQueue is the lane MissionLoop children run on. Empty inherits
	// the supervisor's own task queue.
	MissionTaskQueue string
	// DeliveryTaskQueue is passed through to each MissionLoop child so its own
	// child DeliverPlan executions run on the delivery lane (Task 106).
	DeliveryTaskQueue string
	// CadenceSeconds is the supervisor tick interval. 0 uses 60s.
	CadenceSeconds int
	// MaxIterations bounds history growth before continue-as-new. 0 uses the
	// package default.
	MaxIterations int
	// CarriedIteration preserves the tick counter across continue-as-new.
	CarriedIteration int
}

// PortfolioReconcileInput is the ReconcilePortfolio activity's input. Now is
// passed in from workflow.Now(ctx) so last_scheduled_at is replay-stable.
type PortfolioReconcileInput struct {
	PortfolioID string
	Now         time.Time
}

// ActiveMission carries everything PortfolioLoop needs to (re)start one
// MissionLoop child.
type ActiveMission struct {
	MissionID  string
	WorkflowID string
	Contract   Contract
}

// PortfolioReconcileResult is ReconcilePortfolio's output: the missions that
// must have a running MissionLoop child, plus the fairness accounting the
// supervisor reports.
type PortfolioReconcileResult struct {
	Activated      []string
	Active         []ActiveMission
	ScheduledPick  string
	FairnessSpread int
}

// PortfolioLoop is the durable portfolio supervisor (docs/PLAN.md Task 121). A
// single workflow per portfolio that, on a fixed cadence, admits pending
// missions up to the active-mission cap, ensures every active mission has a
// running MissionLoop child, advances the fair-schedule counter, and
// continues-as-new to bound history. All mutable state (activation, spend,
// schedule) lives in Postgres via ReconcilePortfolio, so the cap, budget
// isolation and fairness spread survive a kill -9 rather than resetting.
//
// Determinism: as with MissionLoop, this function must never call time.Now,
// rand or map iteration -- it uses workflow.Now(ctx) and reads deterministically
// ordered slices returned by its activity.
func PortfolioLoop(ctx workflow.Context, in PortfolioLoopInput) error {
	cadence := time.Duration(in.CadenceSeconds) * time.Second
	if cadence <= 0 {
		cadence = time.Minute
	}
	maxIter := in.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultPortfolioIterations
	}

	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	iteration := in.CarriedIteration
	for {
		var res PortfolioReconcileResult
		err := workflow.ExecuteActivity(ctx, ActivityPortfolioReconcile, PortfolioReconcileInput{
			PortfolioID: in.PortfolioID,
			Now:         workflow.Now(ctx),
		}).Get(ctx, &res)
		if err != nil {
			return fmt.Errorf("portfolio %q: reconcile: %w", in.PortfolioID, err)
		}

		// Ensure a MissionLoop child is running for every active mission. Child
		// workflow IDs are the mission's own deterministic WorkflowID, so a
		// restart never double-activates: starting an already-running child is
		// rejected by Temporal and treated here as "already active".
		for _, m := range res.Active {
			startMissionChild(ctx, in, m)
		}

		iteration++
		if iteration >= maxIter {
			return workflow.NewContinueAsNewError(ctx, PortfolioLoop, PortfolioLoopInput{
				PortfolioID:       in.PortfolioID,
				MissionTaskQueue:  in.MissionTaskQueue,
				DeliveryTaskQueue: in.DeliveryTaskQueue,
				CadenceSeconds:    in.CadenceSeconds,
				MaxIterations:     in.MaxIterations,
				CarriedIteration:  0,
			})
		}
		if err := workflow.Sleep(ctx, cadence); err != nil {
			return fmt.Errorf("portfolio %q: sleep: %w", in.PortfolioID, err)
		}
	}
}

// startMissionChild starts (or re-adopts) one MissionLoop child. It is
// deliberately fire-and-forget: the supervisor does not block on a
// months-long mission, and ParentClosePolicy=ABANDON keeps children running
// across the supervisor's own continue-as-new. An already-running child (same
// deterministic WorkflowID) is left untouched, which is exactly the
// no-double-activation guarantee.
func startMissionChild(ctx workflow.Context, in PortfolioLoopInput, m ActiveMission) {
	childID := m.WorkflowID
	if childID == "" {
		childID = "mission-" + m.MissionID
	}
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:            childID,
		TaskQueue:             in.MissionTaskQueue,
		ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	})
	child := workflow.ExecuteChildWorkflow(childCtx, MissionLoop, MissionLoopInput{
		MissionID:         m.MissionID,
		Contract:          m.Contract,
		DeliveryTaskQueue: in.DeliveryTaskQueue,
	})
	// Wait only for the child to START (not to finish); swallow an
	// already-started error so re-adoption after a restart is a no-op.
	_ = child.GetChildWorkflowExecution().Get(ctx, nil)
}

// MissionContractSource is the subset of *Store the reconcile activity needs to
// resolve a mission's contract for child startup. *Store satisfies it; tests
// substitute a fake.
type MissionContractSource interface {
	GetMission(ctx context.Context, id string) (Mission, error)
}

// PortfolioActivities bundles the side-effecting operations PortfolioLoop calls
// out to. It is the only place the supervisor touches the world.
type PortfolioActivities struct {
	Portfolio *PortfolioStore
	Missions  MissionContractSource
}

// ReconcilePortfolio admits pending missions up to the cap, advances the fair
// schedule by one turn, and returns the active missions (with their contracts)
// the supervisor must keep running. It is the single durable step: every
// mutation it makes is persisted, so the cap/spend/schedule survive a restart.
func (a *PortfolioActivities) ReconcilePortfolio(ctx context.Context, in PortfolioReconcileInput) (PortfolioReconcileResult, error) {
	if a.Portfolio == nil {
		return PortfolioReconcileResult{}, fmt.Errorf("mission: ReconcilePortfolio: nil portfolio store")
	}
	activated, err := a.Portfolio.ActivatePendingUpToCap(ctx, in.PortfolioID)
	if err != nil {
		return PortfolioReconcileResult{}, err
	}

	ids, err := a.Portfolio.ActiveMissionIDs(ctx, in.PortfolioID)
	if err != nil {
		return PortfolioReconcileResult{}, err
	}
	active := make([]ActiveMission, 0, len(ids))
	for _, id := range ids {
		am := ActiveMission{MissionID: id, WorkflowID: "mission-" + id}
		if a.Missions != nil {
			m, err := a.Missions.GetMission(ctx, id)
			if err == nil {
				am.WorkflowID = m.WorkflowID
				am.Contract = m.Contract
			}
		}
		active = append(active, am)
	}

	pick, _, err := a.Portfolio.NextScheduled(ctx, in.PortfolioID, in.Now)
	if err != nil {
		return PortfolioReconcileResult{}, err
	}
	loaded, err := a.Portfolio.Load(ctx, in.PortfolioID)
	if err != nil {
		return PortfolioReconcileResult{}, err
	}
	return PortfolioReconcileResult{
		Activated:      activated,
		Active:         active,
		ScheduledPick:  pick,
		FairnessSpread: loaded.FairnessSpread(),
	}, nil
}
