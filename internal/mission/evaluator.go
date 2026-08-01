package mission

import (
	"context"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// LedgerSample is one payment-provider-ledger observation for a mission,
// covering exactly the fields mission-contract.md §1's net-MRR rule needs:
// net MRR = active recurring subscriptions - refunds - cancellations -
// discounts and credits. Available is false on a
// "payment-data-unavailable" observation (the source itself is
// unreachable or has no reconciled figures yet) -- the evaluator treats
// that as inconclusive, never as a dip.
type LedgerSample struct {
	At                   time.Time
	SubscriptionsUSD     float64
	RefundsUSD           float64
	CancellationsUSD     float64
	DiscountsUSD         float64
	UnrelatedCustomers   int
	RefundChargebackRate float64
	Available            bool
}

// NetMRRUSD computes mission-contract.md §1's net-MRR figure: active
// recurring subscriptions minus refunds, cancellations, and discounts/
// credits.
func (s LedgerSample) NetMRRUSD() float64 {
	return s.SubscriptionsUSD - s.RefundsUSD - s.CancellationsUSD - s.DiscountsUSD
}

// maxPlausibleUSD bounds any single dollar figure a LedgerSample carries.
// Real missions under this profile run on $100-$500 budgets
// (mission-contract.md §1's own worked example); a payment-provider-
// reported figure orders of magnitude beyond that is not a real
// observation to trust blindly. This is a sanity ceiling against garbled/
// adversarial provider input, not a business limit on how large a mission
// could legitimately grow -- a future contract or Task 49's real feed can
// always report a genuinely larger figure than this by raising the
// constant, not by this package silently trusting an unbounded one today.
const maxPlausibleUSD = 1_000_000

// plausible reports whether s's fields are sane enough to drive a real
// evaluation cycle. A payment-provider-ledger sample is untrusted input
// (this task's own ai-vulnerability-defense scope: "treat input to
// net-MRR calculation as untrusted") -- a negative subscriptions/refunds/
// cancellations/discounts figure, a refund/chargeback rate outside the
// physically meaningful [0,1] range, a negative customer count, or any
// dollar figure beyond maxPlausibleUSD are all rejected here. Evaluate
// treats an implausible sample exactly like payment-data-unavailable
// (fail closed: WAITING, never a silent pause/terminate/success decision
// driven off garbage or adversarial data).
//
// This check is dormant today -- UnimplementedNetMRRSource always reports
// Available: false, so Evaluate never reaches this call in practice --
// but becomes load-bearing the instant Task 49 wires in a real payment
// feed, which is exactly why it is added now rather than deferred
// alongside that task.
func (s LedgerSample) plausible() bool {
	for _, v := range []float64{s.SubscriptionsUSD, s.RefundsUSD, s.CancellationsUSD, s.DiscountsUSD} {
		if v < 0 || v > maxPlausibleUSD {
			return false
		}
	}
	if s.UnrelatedCustomers < 0 {
		return false
	}
	if s.RefundChargebackRate < 0 || s.RefundChargebackRate > 1 {
		return false
	}
	return true
}

// NetMRRSource is the payment-provider-ledger integration seam
// (docs/PLAN.md Task 49). This package defines the interface;
// UnimplementedNetMRRSource is the honest stub until cmd/foundryd wires a
// real provider-backed implementation for a given deployment.
type NetMRRSource interface {
	Observe(ctx context.Context, missionID string, at time.Time) (LedgerSample, error)
}

// UnimplementedNetMRRSource is the default when no live ledger integration
// is wired: every call reports payment-data-unavailable rather than
// fabricating a figure. Wiring a real NetMRRSource into cmd/foundryd is
// deployment-specific; until then, missions using this stub pause
// WAITING/provider-outage (payment-data-unavailable), which is honest
// behavior for "no ledger integration configured" rather than a silent
// false success.
type UnimplementedNetMRRSource struct{}

// Observe implements NetMRRSource.
func (UnimplementedNetMRRSource) Observe(_ context.Context, _ string, at time.Time) (LedgerSample, error) {
	return LedgerSample{At: at, Available: false}, nil
}

// EvalState is the evaluator's accumulated, replayable progress across
// Evaluate calls for one mission: the confirmation-window streak plus the
// cycle/no-progress counters Constitution C18's loop bounds require. The
// zero value is a fresh mission.
type EvalState struct {
	// ConfirmedSince is when the current streak of samples meeting target
	// began. nil means the mission is not currently in a confirming
	// streak (mission-contract.md §1: "One payment never triggers
	// success" -- reaching target for one sample only starts the streak).
	ConfirmedSince *time.Time
	// Cycles is the count of real (Available) evaluation cycles seen so
	// far -- checked against Constraints.MaximumValidationCycles.
	Cycles int
	// NoProgressCycles is the count of consecutive real cycles whose net
	// MRR did not exceed the best net MRR seen so far -- checked against
	// Constraints.MaximumNoProgressCycles.
	NoProgressCycles int
	// BestNetMRRUSD is the highest net MRR observed across every real
	// cycle so far.
	BestNetMRRUSD float64
}

// Signal carries the pause/terminate triggers docs/PLAN.md Task 40 Step 3
// wires from outside the evaluator's own net-MRR/confirmation-window
// logic: the cost ledger's budget signal (Task 29) and a human-gate
// escalation (Task 32's internal/recovery pattern). PolicyTerminated is
// the seam for "prohibited-market-detected" (mission-contract.md
// terminate_when): detecting a prohibited market is discovery/marketing
// logic this task's own Boundary excludes, so Evaluate never sets this
// itself -- it only reacts to it if a caller (a future discovery/PEC task)
// raises it.
type Signal struct {
	MonthlyBudgetExhausted bool
	TotalBudgetExhausted   bool
	UnforeseenHumanGate    bool
	PolicyTerminated       bool
	// NoBudgetEnvelope marks that the mission has no provisioned monthly
	// envelope at all (docs/PLAN.md Task 119 / COST-01). It is fail-closed:
	// an unattended mission without an envelope halts rather than running
	// unmetered. It implies MonthlyBudgetExhausted.
	NoBudgetEnvelope bool
}

// Outcome is one Evaluate call's decision. Continue means no pause/
// terminal decision applies yet -- the loop keeps observing on its next
// cadence tick. A non-Continue Outcome's Status/Reason/ResultCode are
// exactly the fields workflow.go needs to build a state.Transition (see
// state.Transition.Validate's invariants: WAITING requires Reason,
// SUCCEEDED forbids Reason, a set ResultCode must be registry-known for
// Status).
type Outcome struct {
	Continue   bool
	Status     state.Status
	Reason     state.Reason
	ResultCode state.ResultCode
}

// postSuccessResultCode maps a Contract's post_success_policy to the
// terminal result code mission-contract.md §2 defines for reaching
// target. decision (no-gaps rule): the doc names five policies
// ("stop | maintenance | raise-target | continue-growth |
// start-another-product") but §2's registry only defines two SUCCEEDED
// codes (MISSION_TARGET_REACHED, MISSION_MAINTENANCE_MODE). The three
// growth-continuation policies (raise-target, continue-growth,
// start-another-product) imply Portfolio-loop behavior owned by tasks not
// yet built (41+); the smallest reversible choice is to resolve them to
// MISSION_TARGET_REACHED — the same terminal code "stop" uses — until a
// later task adds the richer continuation any of the three actually
// requires.
func postSuccessResultCode(policy string) state.ResultCode {
	if policy == PostSuccessMaintenance {
		return state.ResultMissionMaintenanceMode
	}
	return state.ResultMissionTargetReached
}

// Evaluate applies contract's target/confirmation-window/constraint rules
// to one new sample plus any externally-observed pause/terminate signal,
// given prior's accumulated progress. It returns the resulting Outcome and
// the next EvalState (Evaluate is a pure function -- it never mutates
// prior).
//
// Precedence, evaluated in order: a total-budget-exhausted signal
// terminates outright (harder stop than any pause); a policy-terminated
// signal cancels; an unforeseen-human-gate or monthly-budget-exhausted
// signal pauses; an unavailable sample pauses (payment-data-unavailable)
// without touching cycle/streak bookkeeping at all -- a cycle that could
// not actually be evaluated is not a counted cycle, dip, or confirmation.
// Only once none of those apply does a real evaluation cycle run. If this
// sample meets target, the confirmation-window streak grows or completes
// (SUCCEEDED) and the cycle/no-progress counters are left untouched (a
// mission actively confirming success is not "failing to make progress").
// Otherwise the streak resets (a dip, or simply never having met target)
// and the cycle/no-progress counters advance, checked against the
// mechanical no-viable-candidate cycle bounds.
func Evaluate(contract Contract, prior EvalState, sample LedgerSample, sig Signal) (Outcome, EvalState) {
	if sig.TotalBudgetExhausted {
		return Outcome{Status: state.StatusFailed, ResultCode: state.ResultMissionBudgetExhausted}, prior
	}
	if sig.PolicyTerminated {
		return Outcome{Status: state.StatusCancelled, ResultCode: state.ResultMissionTerminatedByPolicy}, prior
	}
	if sig.UnforeseenHumanGate {
		return Outcome{Status: state.StatusWaiting, Reason: state.ReasonUnforeseenHumanGate}, prior
	}
	if sig.MonthlyBudgetExhausted {
		return Outcome{Status: state.StatusWaiting, Reason: state.ReasonBudget}, prior
	}
	if !sample.Available || !sample.plausible() {
		// An implausible sample (negative amounts, an out-of-range refund/
		// chargeback rate, a figure beyond maxPlausibleUSD) is treated
		// identically to an unavailable one -- see LedgerSample.plausible's
		// doc comment: untrusted provider input never drives a pause/
		// terminate/success decision, it only ever produces the same safe
		// WAITING/provider-outage fallback a genuinely missing reading
		// would.
		//
		// decision (no-gaps rule): "payment-data-unavailable" has no
		// dedicated entry in state-model.md's closed wait-reason registry
		// (internal/state/registries_test.go diffs that registry against
		// the governing doc verbatim -- adding one here would require
		// editing that doc, out of this task's scope). mission-
		// contract.md's own vocabulary calls the net-MRR source a
		// "payment-provider-ledger" -- a provider -- so ReasonProviderOutage
		// ("provider-outage", WAITING_FOR_PROVIDER) is the closest
		// registry-known reason for "this provider's data is unavailable",
		// reused rather than inventing new plumbing.
		return Outcome{Status: state.StatusWaiting, Reason: state.ReasonProviderOutage}, prior
	}

	next := prior
	netMRR := sample.NetMRRUSD()

	targetMet := netMRR >= contract.Target.AmountUSD &&
		sample.UnrelatedCustomers >= contract.Target.MinimumUnrelatedPayingCustomers &&
		sample.RefundChargebackRate < contract.Target.RefundChargebackRateBelow

	if targetMet {
		if next.ConfirmedSince == nil {
			at := sample.At
			next.ConfirmedSince = &at
		}
		window, err := parseDuration(contract.Target.ConfirmationWindow)
		if err == nil && !sample.At.Before(next.ConfirmedSince.Add(window)) {
			return Outcome{Status: state.StatusSucceeded, ResultCode: postSuccessResultCode(contract.PostSuccessPolicy)}, next
		}
		// mission-contract.md §1: "One payment never triggers success" --
		// the streak has started (or continued) but has not yet spanned
		// the full confirmation_window, so the loop keeps observing.
		// Constraints.Maximum{Validation,NoProgress}Cycles deliberately do
		// not advance while a streak is actively confirming (see this
		// function's own doc comment) -- those bounds cap how long the
		// mission may keep *trying* without meeting target at all, not how
		// long a confirmed streak may take to finish confirming.
		return Outcome{Continue: true}, next
	}

	// A dip below target (or a cycle that never met target at all) resets
	// the confirmation-window streak -- mission-contract.md §1's
	// confirmation_window is a *continuous* requirement, not a cumulative
	// one -- and counts as one validation cycle toward Constitution C18's
	// mechanical loop bounds.
	next.ConfirmedSince = nil
	next.Cycles++
	if netMRR > prior.BestNetMRRUSD {
		next.BestNetMRRUSD = netMRR
		next.NoProgressCycles = 0
	} else {
		next.NoProgressCycles++
	}

	if contract.Constraints.MaximumNoProgressCycles > 0 && next.NoProgressCycles >= contract.Constraints.MaximumNoProgressCycles {
		return Outcome{Status: state.StatusFailed, ResultCode: state.ResultMissionNoViableCandidate}, next
	}
	if contract.Constraints.MaximumValidationCycles > 0 && next.Cycles >= contract.Constraints.MaximumValidationCycles {
		return Outcome{Status: state.StatusFailed, ResultCode: state.ResultMissionNoViableCandidate}, next
	}
	return Outcome{Continue: true}, next
}
