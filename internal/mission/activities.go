package mission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

// BudgetStore is the subset of internal/ledger/cost.Store's read behavior
// CheckBudget depends on (interfaces defined in the consuming package,
// matching internal/kernel/budget.go's own BudgetStore pattern).
type BudgetStore interface {
	GetBudget(ctx context.Context, scope cost.Scope, scopeID string, kind cost.Kind, period string) (cost.Budget, error)
}

// LoopContractChecker is the subset of *Store's behavior RequireLoopContract
// depends on (interfaces defined in the consuming package -- *Store
// satisfies this structurally; tests substitute an in-memory fake so
// go test ./internal/mission/... never needs a live Postgres).
type LoopContractChecker interface {
	HasLoopContract(ctx context.Context, loopName string) (bool, error)
}

// MissionStateRecorder is the subset of *Store's behavior
// RecordMissionState depends on.
type MissionStateRecorder interface {
	RecordState(ctx context.Context, snap StateSnapshot) error
}

// GateEventRecorder is the subset of *Store's behavior RecordGateEvent
// depends on.
type GateEventRecorder interface {
	RecordGateEvent(ctx context.Context, missionID, action string, occurredAt time.Time) (string, error)
}

// experimentBudgetPeriod is the fixed budgets.period key a mission's
// total_experiment_usd envelope (cost.KindExperiment) is provisioned
// under: unlike the mission_monthly envelope, mission-contract.md's
// total_experiment_usd is a lifetime cap, not a calendar-scoped one, and
// the budgets table has no "no period" sentinel -- decision (no-gaps
// rule): a fixed literal period key is the smallest reversible choice.
const experimentBudgetPeriod = "lifetime"

func currentPeriod(t time.Time) string { return t.Format("2006-01") }

// Activities bundles every side-effecting operation MissionLoop calls out
// to. It is the only place in this package that touches the world --
// workflow.go must never construct or call these directly outside of
// workflow.ExecuteActivity (mirrors internal/kernel/activities.go's own
// separation).
type Activities struct {
	LoopContracts LoopContractChecker
	MissionState  MissionStateRecorder
	GateEvents    GateEventRecorder
	Transitions   kernel.TransitionStore
	Budgets       BudgetStore
	NetMRRSource  NetMRRSource
}

// NewActivities builds an Activities set from its dependencies. A single
// *Store satisfies LoopContractChecker, MissionStateRecorder, and
// GateEventRecorder simultaneously, so production wiring (cmd/foundryd)
// passes the same *Store for all three.
func NewActivities(loopContracts LoopContractChecker, missionState MissionStateRecorder, gateEvents GateEventRecorder, transitions kernel.TransitionStore, budgets BudgetStore, netMRRSource NetMRRSource) *Activities {
	return &Activities{
		LoopContracts: loopContracts,
		MissionState:  missionState,
		GateEvents:    gateEvents,
		Transitions:   transitions,
		Budgets:       budgets,
		NetMRRSource:  netMRRSource,
	}
}

// RequireLoopContract implements ActivityRequireLoopContract:
// mission-contract.md §3's "every loop MUST register a loop contract"
// requirement, enforced as a hard refusal-to-start.
func (a *Activities) RequireLoopContract(ctx context.Context, missionID string) error {
	ok, err := a.LoopContracts.HasLoopContract(ctx, loopName(missionID))
	if err != nil {
		return fmt.Errorf("mission: require loop contract for %s: %w", missionID, err)
	}
	if !ok {
		return fmt.Errorf("mission: %s has no registered loop_contracts row -- refusing to start (mission-contract.md §3)", missionID)
	}
	return nil
}

// ObserveLedger implements ActivityObserveLedger: the one call site for
// this mission's NetMRRSource (the Task-49 seam evaluator.go defines).
func (a *Activities) ObserveLedger(ctx context.Context, missionID string) (LedgerSample, error) {
	sample, err := a.NetMRRSource.Observe(ctx, missionID, time.Now().UTC())
	if err != nil {
		return LedgerSample{}, fmt.Errorf("mission: observe ledger for %s: %w", missionID, err)
	}
	return sample, nil
}

// CheckBudget implements ActivityCheckBudget: the docs/PLAN.md Task 40
// Step 3 budget signal, read from Task 29's cost ledger. A scope with no
// envelope provisioned yet is treated as unmetered (not exhausted) --
// the same precedent internal/kernel.Activities.ReserveBudget already
// established for cost.ErrBudgetNotFound.
func (a *Activities) CheckBudget(ctx context.Context, missionID string) (Signal, error) {
	var sig Signal

	monthly, err := a.Budgets.GetBudget(ctx, cost.ScopeMission, missionID, cost.KindMissionMonthly, currentPeriod(time.Now()))
	switch {
	case errors.Is(err, cost.ErrBudgetNotFound):
	case err != nil:
		return Signal{}, fmt.Errorf("mission: check monthly budget for %s: %w", missionID, err)
	default:
		sig.MonthlyBudgetExhausted = monthly.CeilingUSD-(monthly.ReservedUSD+monthly.IncurredUSD) <= 0
	}

	total, err := a.Budgets.GetBudget(ctx, cost.ScopeMission, missionID, cost.KindExperiment, experimentBudgetPeriod)
	switch {
	case errors.Is(err, cost.ErrBudgetNotFound):
	case err != nil:
		return Signal{}, fmt.Errorf("mission: check total experiment budget for %s: %w", missionID, err)
	default:
		sig.TotalBudgetExhausted = total.CeilingUSD-(total.ReservedUSD+total.IncurredUSD) <= 0
	}

	return sig, nil
}

// AppendMissionTransition implements ActivityAppendMissionTransition.
func (a *Activities) AppendMissionTransition(ctx context.Context, in AppendTransitionInput) error {
	if _, err := a.Transitions.Append(ctx, in.WorkflowID, in.Transition); err != nil {
		return fmt.Errorf("mission: append transition for %s: %w", in.WorkflowID, err)
	}
	return nil
}

// RecordMissionState implements ActivityRecordMissionState: the
// mission_state append-only audit row for one evaluator cycle.
func (a *Activities) RecordMissionState(ctx context.Context, in missionStateInput) error {
	snap := StateSnapshot{
		MissionID:        in.MissionID,
		Cycle:            in.EvalState.Cycles,
		NetMRRUSD:        in.Sample.NetMRRUSD(),
		NoProgressCycles: in.EvalState.NoProgressCycles,
		Confirming:       in.EvalState.ConfirmedSince != nil,
		ConfirmedSince:   in.EvalState.ConfirmedSince,
		Status:           string(in.Outcome.Status),
		Reason:           string(in.Outcome.Reason),
		ResultCode:       string(in.Outcome.ResultCode),
		ObservedAt:       in.At,
	}
	if err := a.MissionState.RecordState(ctx, snap); err != nil {
		return fmt.Errorf("mission: record state for %s: %w", in.MissionID, err)
	}
	return nil
}

// RecordGateEvent implements ActivityRecordGateEvent: mirrors Task 32's
// internal/recovery human-gate escalation pattern, applied to a mission's
// own unforeseen-human-gate pause.
func (a *Activities) RecordGateEvent(ctx context.Context, missionID string) error {
	if _, err := a.GateEvents.RecordGateEvent(ctx, missionID, PauseUnforeseenHumanGate, time.Now().UTC()); err != nil {
		return fmt.Errorf("mission: record gate event for %s: %w", missionID, err)
	}
	return nil
}
