package mission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

// deterministicID derives a stable row id from a workflow ID and loop
// iteration (and prefix), so a retried mission activity writes the SAME id and
// ON CONFLICT DO NOTHING turns the retry into a no-op rather than a duplicate
// row (docs/PLAN.md Task 122). It is a pure function, safe to call from
// deterministic workflow code as well as from the activity.
func deterministicID(prefix, workflowID string, iteration int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", prefix, workflowID, iteration)))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}

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

// ReadinessChecker is the subset of *Store's behavior RequireReadiness
// depends on.
type ReadinessChecker interface {
	HasPassingReadinessArtifact(ctx context.Context, missionID string) (bool, error)
}

// MissionStateRecorder is the subset of *Store's behavior
// RecordMissionState depends on. RecordStateWithID is the idempotent,
// deterministic-id variant docs/PLAN.md Task 122 uses.
type MissionStateRecorder interface {
	RecordState(ctx context.Context, snap StateSnapshot) error
	RecordStateWithID(ctx context.Context, id string, snap StateSnapshot) error
}

// GateEventRecorder is the subset of *Store's behavior RecordGateEvent
// depends on. RecordGateEventWithID is the idempotent, deterministic-id
// variant docs/PLAN.md Task 122 uses so a retried escalation addresses the
// same gate_events row.
type GateEventRecorder interface {
	RecordGateEvent(ctx context.Context, missionID, action string, occurredAt time.Time) (string, error)
	RecordGateEventWithID(ctx context.Context, id, missionID, action string, occurredAt time.Time) error
}

// GateEventResolver is the subset of *Store's behavior ResolveGateEvent
// depends on.
type GateEventResolver interface {
	ResolveGateEvent(ctx context.Context, id, resolution string, resolvedAt time.Time) error
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
	Readiness     ReadinessChecker
	MissionState  MissionStateRecorder
	GateEvents    GateEventRecorder
	ResolveGates  GateEventResolver
	Transitions   kernel.TransitionStore
	Budgets       BudgetStore
	NetMRRSource  NetMRRSource
	// Receipts is the kernel's idempotency-receipt store (Task 122): every
	// state-mutating mission activity runs through kernel.WithReceipt keyed on
	// {WorkflowID, loop iteration, activity, attempt}, so a Temporal-level
	// retry of a call whose Postgres write already committed returns the
	// recorded receipt instead of producing a duplicate audit row. A nil
	// Receipts is treated as a MemReceiptStore so unit tests and any run
	// without Postgres still exercise the wrapped path.
	Receipts kernel.ReceiptStore
}

// NewActivities builds an Activities set from its dependencies. A single
// *Store satisfies LoopContractChecker, MissionStateRecorder, and
// GateEventRecorder simultaneously, so production wiring (cmd/foundryd)
// passes the same *Store for all three.
func NewActivities(loopContracts LoopContractChecker, readiness ReadinessChecker, missionState MissionStateRecorder, gateEvents GateEventRecorder, resolveGates GateEventResolver, transitions kernel.TransitionStore, budgets BudgetStore, netMRRSource NetMRRSource, receipts kernel.ReceiptStore) *Activities {
	if receipts == nil {
		receipts = kernel.NewMemReceiptStore()
	}
	return &Activities{
		LoopContracts: loopContracts,
		Readiness:     readiness,
		MissionState:  missionState,
		GateEvents:    gateEvents,
		ResolveGates:  resolveGates,
		Transitions:   transitions,
		Budgets:       budgets,
		NetMRRSource:  netMRRSource,
		Receipts:      receipts,
	}
}

// receipts returns the configured receipt store, defaulting to an in-memory one
// so an Activities built as a struct literal (tests) is still safe to call.
func (a *Activities) receipts() kernel.ReceiptStore {
	if a.Receipts == nil {
		a.Receipts = kernel.NewMemReceiptStore()
	}
	return a.Receipts
}

// missionReceiptKey builds the idempotency key for one mission activity
// invocation. The loop iteration is the mission analogue of the kernel's task
// ID (docs/PLAN.md Task 122).
func missionReceiptKey(workflowID string, iteration, attempt int, activity string) string {
	return kernel.IdempotencyKey{
		WorkflowID: workflowID,
		TaskID:     fmt.Sprintf("iter-%d", iteration),
		Activity:   activity,
		Attempt:    attempt,
	}.String()
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

// RequireReadiness implements ActivityRequireReadiness. MissionLoop may not
// start unattended execution until ceremony readiness is passing.
func (a *Activities) RequireReadiness(ctx context.Context, missionID string) error {
	ok, err := a.Readiness.HasPassingReadinessArtifact(ctx, missionID)
	if err != nil {
		return fmt.Errorf("mission: require readiness for %s: %w", missionID, err)
	}
	if !ok {
		return fmt.Errorf("mission: %s has no passing readiness artifact -- refusing to start unattended runtime", missionID)
	}
	return nil
}

// ObserveLedger implements ActivityObserveLedger: the one call site for
// this mission's NetMRRSource (the Task-49 seam evaluator.go defines). The
// observation instant is passed IN from workflow.Now(ctx) rather than read
// with time.Now() here, so a retried observation samples the same instant and
// is reproducible on replay (docs/PLAN.md Task 122 step 4).
func (a *Activities) ObserveLedger(ctx context.Context, in observeLedgerInput) (LedgerSample, error) {
	at := in.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	sample, err := a.NetMRRSource.Observe(ctx, in.MissionID, at.UTC())
	if err != nil {
		return LedgerSample{}, fmt.Errorf("mission: observe ledger for %s: %w", in.MissionID, err)
	}
	return sample, nil
}

// CheckBudget implements ActivityCheckBudget: the docs/PLAN.md Task 40
// Step 3 budget signal, read from Task 29's cost ledger. A scope with no
// envelope provisioned yet is treated as unmetered (not exhausted) --
// the same precedent internal/kernel.Activities.ReserveBudget already
// established for cost.ErrBudgetNotFound.
// CheckBudget reports whether a mission's monthly or total experiment budget
// is exhausted. docs/PLAN.md Task 119 (COST-01): a mission is unattended by
// nature, so "no envelope" is a REFUSAL, not "unmetered" — the absence of a
// monthly envelope is treated as exhausted so the mission halts rather than
// running unbounded.
func (a *Activities) CheckBudget(ctx context.Context, missionID string) (Signal, error) {
	var sig Signal

	monthly, err := a.Budgets.GetBudget(ctx, cost.ScopeMission, missionID, cost.KindMissionMonthly, currentPeriod(time.Now()))
	switch {
	case errors.Is(err, cost.ErrBudgetNotFound):
		// Fail closed: an unattended mission with no monthly envelope must not
		// run unmetered (Task 119 / C19/C24).
		sig.MonthlyBudgetExhausted = true
		sig.NoBudgetEnvelope = true
	case err != nil:
		return Signal{}, fmt.Errorf("mission: check monthly budget for %s: %w", missionID, err)
	default:
		sig.MonthlyBudgetExhausted = monthly.CeilingUSD-(monthly.ReservedUSD+monthly.IncurredUSD) <= 0
	}

	total, err := a.Budgets.GetBudget(ctx, cost.ScopeMission, missionID, cost.KindExperiment, experimentBudgetPeriod)
	switch {
	case errors.Is(err, cost.ErrBudgetNotFound):
		// The experiment cap is optional; its absence alone does not halt the
		// mission (the monthly envelope above is the mandatory one). Recorded
		// as a decision (no-gaps rule) rather than silently ignored.
	case err != nil:
		return Signal{}, fmt.Errorf("mission: check total experiment budget for %s: %w", missionID, err)
	default:
		sig.TotalBudgetExhausted = total.CeilingUSD-(total.ReservedUSD+total.IncurredUSD) <= 0
	}

	return sig, nil
}

// AppendMissionTransition implements ActivityAppendMissionTransition. It runs
// through kernel.WithReceipt so a Temporal-level retry of a call whose
// transition write already committed returns the recorded receipt instead of
// appending a duplicate audit row (docs/PLAN.md Task 122).
func (a *Activities) AppendMissionTransition(ctx context.Context, in AppendTransitionInput) error {
	// The key is derived from the transition's own replay-stable content
	// (target status, reason, result code, and workflow.Now-stamped instant),
	// so two genuinely distinct transitions get distinct keys while a Temporal
	// retry of the SAME transition — identical fields on replay — reuses the
	// key and is deduplicated.
	t := in.Transition
	key := fmt.Sprintf("mtrans|%s|%s|%s|%s|%d", in.WorkflowID, t.Status, t.Reason, t.ResultCode, t.OccurredAt.UTC().UnixNano())
	_, err := kernel.WithReceipt(ctx, a.receipts(), key, func() (struct{}, error) {
		if _, err := a.Transitions.Append(ctx, in.WorkflowID, in.Transition); err != nil {
			return struct{}{}, fmt.Errorf("mission: append transition for %s: %w", in.WorkflowID, err)
		}
		return struct{}{}, nil
	})
	return err
}

// RecordMissionState implements ActivityRecordMissionState: the
// mission_state append-only audit row for one evaluator cycle. Wrapped in
// kernel.WithReceipt AND written under a deterministic id derived from the
// loop iteration, so neither an ordinary retry (this activity runs under
// MaximumAttempts:3) nor a commit-then-crash produces a duplicate row.
func (a *Activities) RecordMissionState(ctx context.Context, in MissionStateInput) error {
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
	stateID := deterministicID("mstate", in.WorkflowID, in.LoopIteration)
	key := missionReceiptKey(in.WorkflowID, in.LoopIteration, in.Attempt, ActivityRecordMissionState)
	_, err := kernel.WithReceipt(ctx, a.receipts(), key, func() (struct{}, error) {
		if err := a.MissionState.RecordStateWithID(ctx, stateID, snap); err != nil {
			return struct{}{}, fmt.Errorf("mission: record state for %s: %w", in.MissionID, err)
		}
		return struct{}{}, nil
	})
	return err
}

// RecordGateEvent implements ActivityRecordGateEvent: mirrors Task 32's
// internal/recovery human-gate escalation pattern, applied to a mission's
// own unforeseen-human-gate pause. The gate id is deterministic (the caller
// derives it from missionID+iteration+gate kind), so a retry addresses the
// same row and the workflow's later ResolveGateEvent closes exactly it.
func (a *Activities) RecordGateEvent(ctx context.Context, in GateEventInput) (string, error) {
	if strings.TrimSpace(in.Action) == "" {
		in.Action = PauseUnforeseenHumanGate
	}
	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	gateID := in.GateEventID
	if gateID == "" {
		gateID = deterministicID("gate", in.WorkflowID, in.LoopIteration)
	}
	key := missionReceiptKey(in.WorkflowID, in.LoopIteration, in.Attempt, ActivityRecordGateEvent)
	return kernel.WithReceipt(ctx, a.receipts(), key, func() (string, error) {
		if err := a.GateEvents.RecordGateEventWithID(ctx, gateID, in.MissionID, in.Action, occurredAt.UTC()); err != nil {
			return "", fmt.Errorf("mission: record gate event for %s: %w", in.MissionID, err)
		}
		return gateID, nil
	})
}

// ResolveGateEvent implements ActivityResolveGateEvent. The resolve is
// idempotent (a repeated UPDATE sets the same resolved_at); wrapping it in
// kernel.WithReceipt additionally short-circuits a Temporal retry.
func (a *Activities) ResolveGateEvent(ctx context.Context, in ResolveGateInput) error {
	resolvedAt := in.ResolvedAt
	if resolvedAt.IsZero() {
		resolvedAt = time.Now().UTC()
	}
	key := missionReceiptKey(in.WorkflowID, in.LoopIteration, in.Attempt, ActivityResolveGateEvent)
	_, err := kernel.WithReceipt(ctx, a.receipts(), key, func() (struct{}, error) {
		if err := a.ResolveGates.ResolveGateEvent(ctx, in.GateEventID, in.Resolution, resolvedAt.UTC()); err != nil {
			return struct{}{}, fmt.Errorf("mission: resolve gate event %s: %w", in.GateEventID, err)
		}
		return struct{}{}, nil
	})
	return err
}
