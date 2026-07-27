package mission

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// testContract mirrors the doc's USD-100 example (contract_test.go's
// usd100ExampleYAML), used directly as a Go value so evaluator tests don't
// need to go through YAML parsing.
func testContract() Contract {
	return Contract{
		ID:        "m1",
		Statement: "Reach at least USD 100 in verified net monthly recurring revenue.",
		Target: Target{
			Metric:                          "net_mrr",
			Source:                          "payment-provider-ledger",
			Verification:                    "reconciled",
			AmountUSD:                       100,
			ConfirmationWindow:              "30d",
			MinimumUnrelatedPayingCustomers: 3,
			RefundChargebackRateBelow:       0.05,
		},
		Budget:  Budget{MonthlyUSD: 100, TotalExperimentUSD: 500},
		Cadence: Cadence{Observe: "daily", Improve: "weekly"},
		Constraints: Constraints{
			MaximumActiveProducts:   1,
			MaximumValidationCycles: 12,
			MaximumNoProgressCycles: 4,
		},
		PauseWhen:         []string{PauseMonthlyBudgetExhausted, PausePaymentDataUnavailable, PauseUnforeseenHumanGate},
		TerminateWhen:     []string{TerminateTotalBudgetExhausted, TerminateProhibitedMarket, TerminateNoViableCandidate},
		PostSuccessPolicy: PostSuccessStop,
	}
}

var day0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// meetingSample returns a LedgerSample whose net MRR, customer count, and
// refund/chargeback rate all satisfy testContract's target, at day0+offset
// days.
func meetingSample(offsetDays int) LedgerSample {
	return LedgerSample{
		At:                   day0.Add(time.Duration(offsetDays) * 24 * time.Hour),
		SubscriptionsUSD:     150,
		RefundsUSD:           20,
		CancellationsUSD:     10,
		DiscountsUSD:         10, // net = 110 >= 100
		UnrelatedCustomers:   5,
		RefundChargebackRate: 0.01,
		Available:            true,
	}
}

// belowSample returns a LedgerSample whose net MRR is below testContract's
// target.
func belowSample(offsetDays int) LedgerSample {
	s := meetingSample(offsetDays)
	s.SubscriptionsUSD = 50 // net = 10, well below 100
	return s
}

func TestEvaluate_BelowTarget_Continues(t *testing.T) {
	out, next := Evaluate(testContract(), EvalState{}, belowSample(0), Signal{})
	if !out.Continue {
		t.Fatalf("Outcome = %+v, want Continue", out)
	}
	if next.ConfirmedSince != nil {
		t.Fatalf("ConfirmedSince = %v, want nil (never met target)", next.ConfirmedSince)
	}
	if next.Cycles != 1 {
		t.Fatalf("Cycles = %d, want 1", next.Cycles)
	}
}

// TestEvaluate_SinglePaymentNotSuccess proves mission-contract.md §1's "one
// payment never triggers success": a single sample meeting target starts
// the confirmation streak but does not, by itself, reach SUCCEEDED.
func TestEvaluate_SinglePaymentNotSuccess(t *testing.T) {
	out, next := Evaluate(testContract(), EvalState{}, meetingSample(0), Signal{})
	if !out.Continue {
		t.Fatalf("Outcome = %+v, want Continue (single sample must not succeed)", out)
	}
	if next.ConfirmedSince == nil {
		t.Fatal("ConfirmedSince = nil, want streak started")
	}
}

// TestEvaluate_SustainedWindow_Succeeds proves a streak spanning the full
// 30d confirmation_window reaches SUCCEEDED/MISSION_TARGET_REACHED.
func TestEvaluate_SustainedWindow_Succeeds(t *testing.T) {
	c := testContract()
	evalState := EvalState{}
	var out Outcome
	for day := 0; day <= 30; day++ {
		out, evalState = Evaluate(c, evalState, meetingSample(day), Signal{})
		if day < 30 && !out.Continue {
			t.Fatalf("day %d: Outcome = %+v, want Continue (window not yet elapsed)", day, out)
		}
	}
	if out.Continue {
		t.Fatal("final day: want terminal Outcome, got Continue")
	}
	if out.Status != state.StatusSucceeded {
		t.Fatalf("Status = %q, want SUCCEEDED", out.Status)
	}
	if out.ResultCode != state.ResultMissionTargetReached {
		t.Fatalf("ResultCode = %q, want %q", out.ResultCode, state.ResultMissionTargetReached)
	}
}

// TestEvaluate_WindowResetOnDip proves a dip mid-streak resets the
// confirmation window: 20 days meeting target, one dip, then only a fresh
// 30-day streak (not the pre-dip days) counts toward success.
func TestEvaluate_WindowResetOnDip(t *testing.T) {
	c := testContract()
	evalState := EvalState{}

	for day := 0; day < 20; day++ {
		_, evalState = Evaluate(c, evalState, meetingSample(day), Signal{})
	}
	if evalState.ConfirmedSince == nil {
		t.Fatal("expected an active streak before the dip")
	}

	// Day 20: a dip below target.
	out, evalState := Evaluate(c, evalState, belowSample(20), Signal{})
	if !out.Continue {
		t.Fatalf("dip day: Outcome = %+v, want Continue", out)
	}
	if evalState.ConfirmedSince != nil {
		t.Fatal("ConfirmedSince: want reset to nil after a dip")
	}

	// Days 21..49 (29 more days) meeting target again: the pre-dip streak
	// must NOT count, so day 21+29=50 is the earliest success, not
	// day 20+30=50 either way in this arithmetic -- the real proof is that
	// day 49 (only 29 days after the reset) must still be Continue.
	for day := 21; day <= 49; day++ {
		out, evalState = Evaluate(c, evalState, meetingSample(day), Signal{})
	}
	if !out.Continue {
		t.Fatalf("day 49 (29 days after reset): Outcome = %+v, want Continue (window not yet re-elapsed)", out)
	}

	out, _ = Evaluate(c, evalState, meetingSample(51), Signal{})
	if out.Continue {
		t.Fatal("day 51 (30+ days after reset): want terminal Outcome, got Continue")
	}
	if out.Status != state.StatusSucceeded {
		t.Fatalf("Status = %q, want SUCCEEDED", out.Status)
	}
}

func TestEvaluate_BelowMinimumCustomers_NotConfirmed(t *testing.T) {
	s := meetingSample(0)
	s.UnrelatedCustomers = 1 // below MinimumUnrelatedPayingCustomers: 3
	out, next := Evaluate(testContract(), EvalState{}, s, Signal{})
	if !out.Continue {
		t.Fatalf("Outcome = %+v, want Continue (too few customers)", out)
	}
	if next.ConfirmedSince != nil {
		t.Fatal("ConfirmedSince: want nil, target not actually met")
	}
}

func TestEvaluate_RefundRateAboveThreshold_NotConfirmed(t *testing.T) {
	s := meetingSample(0)
	s.RefundChargebackRate = 0.10 // above RefundChargebackRateBelow: 0.05
	out, next := Evaluate(testContract(), EvalState{}, s, Signal{})
	if !out.Continue {
		t.Fatalf("Outcome = %+v, want Continue (refund rate too high)", out)
	}
	if next.ConfirmedSince != nil {
		t.Fatal("ConfirmedSince: want nil, target not actually met")
	}
}

func TestEvaluate_PaymentDataUnavailable_Pauses(t *testing.T) {
	prior := EvalState{Cycles: 3, BestNetMRRUSD: 50}
	sample := LedgerSample{At: day0, Available: false}
	out, next := Evaluate(testContract(), prior, sample, Signal{})
	if out.Status != state.StatusWaiting || out.Reason != state.ReasonProviderOutage {
		t.Fatalf("Outcome = %+v, want WAITING/provider-outage", out)
	}
	if next != prior {
		t.Fatalf("EvalState = %+v, want unchanged %+v (an unavailable cycle is not a counted cycle)", next, prior)
	}
}

// TestEvaluate_ImplausibleSample_TreatedAsUnavailable proves the
// defense-in-depth input validation on LedgerSample: an untrusted
// payment-provider-ledger reading with negative amounts, an out-of-range
// refund/chargeback rate, a negative customer count, or an implausibly
// large figure never drives a pause/terminate/success decision -- it is
// treated exactly like payment-data-unavailable (WAITING/provider-outage,
// EvalState untouched), the same fail-closed outcome as a genuinely
// missing reading.
func TestEvaluate_ImplausibleSample_TreatedAsUnavailable(t *testing.T) {
	base := meetingSample(0) // otherwise a plausible, target-meeting sample

	tests := []struct {
		name   string
		mutate func(s *LedgerSample)
	}{
		{"negative refunds", func(s *LedgerSample) { s.RefundsUSD = -50 }},
		{"negative subscriptions", func(s *LedgerSample) { s.SubscriptionsUSD = -1 }},
		{"negative cancellations", func(s *LedgerSample) { s.CancellationsUSD = -1 }},
		{"negative discounts", func(s *LedgerSample) { s.DiscountsUSD = -1 }},
		{"negative customer count", func(s *LedgerSample) { s.UnrelatedCustomers = -1 }},
		{"refund rate above 1", func(s *LedgerSample) { s.RefundChargebackRate = 1.5 }},
		{"refund rate negative", func(s *LedgerSample) { s.RefundChargebackRate = -0.1 }},
		{"implausibly large subscriptions", func(s *LedgerSample) { s.SubscriptionsUSD = maxPlausibleUSD + 1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample := base
			tt.mutate(&sample)
			prior := EvalState{Cycles: 3, BestNetMRRUSD: 50}

			out, next := Evaluate(testContract(), prior, sample, Signal{})
			if out.Status != state.StatusWaiting || out.Reason != state.ReasonProviderOutage {
				t.Fatalf("Outcome = %+v, want WAITING/provider-outage (fail closed on implausible input)", out)
			}
			if next != prior {
				t.Fatalf("EvalState = %+v, want unchanged %+v (an implausible sample must not count as a real cycle)", next, prior)
			}
		})
	}
}

func TestEvaluate_MonthlyBudgetExhausted_Pauses(t *testing.T) {
	out, _ := Evaluate(testContract(), EvalState{}, meetingSample(0), Signal{MonthlyBudgetExhausted: true})
	if out.Status != state.StatusWaiting || out.Reason != state.ReasonBudget {
		t.Fatalf("Outcome = %+v, want WAITING/budget", out)
	}
}

func TestEvaluate_UnforeseenHumanGate_Pauses(t *testing.T) {
	out, _ := Evaluate(testContract(), EvalState{}, meetingSample(0), Signal{UnforeseenHumanGate: true})
	if out.Status != state.StatusWaiting || out.Reason != state.ReasonUnforeseenHumanGate {
		t.Fatalf("Outcome = %+v, want WAITING/unforeseen-human-gate", out)
	}
}

func TestEvaluate_TotalBudgetExhausted_Terminates(t *testing.T) {
	// Precedence: even with other pause signals also set, total-budget
	// exhaustion is the harder stop and takes priority.
	sig := Signal{TotalBudgetExhausted: true, MonthlyBudgetExhausted: true, UnforeseenHumanGate: true}
	out, _ := Evaluate(testContract(), EvalState{}, meetingSample(0), sig)
	if out.Status != state.StatusFailed || out.ResultCode != state.ResultMissionBudgetExhausted {
		t.Fatalf("Outcome = %+v, want FAILED/MISSION_BUDGET_EXHAUSTED", out)
	}
}

func TestEvaluate_PolicyTerminated_Cancels(t *testing.T) {
	out, _ := Evaluate(testContract(), EvalState{}, meetingSample(0), Signal{PolicyTerminated: true})
	if out.Status != state.StatusCancelled || out.ResultCode != state.ResultMissionTerminatedByPolicy {
		t.Fatalf("Outcome = %+v, want CANCELLED/MISSION_TERMINATED_BY_POLICY", out)
	}
}

func TestEvaluate_MaxValidationCycles_NoViableCandidate(t *testing.T) {
	c := testContract()
	c.Constraints.MaximumNoProgressCycles = 1000 // isolate the validation-cycle bound
	evalState := EvalState{}
	var out Outcome
	for day := 0; day < c.Constraints.MaximumValidationCycles; day++ {
		out, evalState = Evaluate(c, evalState, belowSample(day), Signal{})
	}
	if out.Continue {
		t.Fatal("want terminal Outcome once maximum_validation_cycles is reached")
	}
	if out.Status != state.StatusFailed || out.ResultCode != state.ResultMissionNoViableCandidate {
		t.Fatalf("Outcome = %+v, want FAILED/MISSION_NO_VIABLE_CANDIDATE", out)
	}
}

func TestEvaluate_MaxNoProgressCycles_NoViableCandidate(t *testing.T) {
	c := testContract()
	c.Constraints.MaximumValidationCycles = 1000 // isolate the no-progress bound
	evalState := EvalState{}
	var out Outcome
	// The first belowSample call always "progresses" (net MRR 10 > the
	// zero-value BestNetMRRUSD), resetting the counter once -- so reaching
	// the threshold takes MaximumNoProgressCycles+1 total calls, not
	// MaximumNoProgressCycles.
	for day := 0; day < c.Constraints.MaximumNoProgressCycles+1; day++ {
		out, evalState = Evaluate(c, evalState, belowSample(day), Signal{})
	}
	if out.Status != state.StatusFailed || out.ResultCode != state.ResultMissionNoViableCandidate {
		t.Fatalf("Outcome = %+v, want FAILED/MISSION_NO_VIABLE_CANDIDATE", out)
	}
}

func TestEvaluate_ProgressResetsNoProgressCounter(t *testing.T) {
	c := testContract()
	evalState := EvalState{}
	// day0's net MRR (10) exceeds the zero-value BestNetMRRUSD, so it
	// counts as "progress" and resets the counter once; day1 repeats the
	// same net MRR, which is not an improvement, so the counter advances
	// to 1.
	_, evalState = Evaluate(c, evalState, belowSample(0), Signal{})
	_, evalState = Evaluate(c, evalState, belowSample(1), Signal{})
	if evalState.NoProgressCycles != 1 {
		t.Fatalf("NoProgressCycles = %d, want 1", evalState.NoProgressCycles)
	}

	// A sample with strictly higher net MRR than any seen so far resets
	// the no-progress counter, even though it still doesn't meet target.
	improving := belowSample(2)
	improving.SubscriptionsUSD = 70 // net = 30 > best-so-far (10)
	_, evalState = Evaluate(c, evalState, improving, Signal{})
	if evalState.NoProgressCycles != 0 {
		t.Fatalf("NoProgressCycles = %d, want 0 after progress", evalState.NoProgressCycles)
	}
	if evalState.BestNetMRRUSD != 30 {
		t.Fatalf("BestNetMRRUSD = %v, want 30", evalState.BestNetMRRUSD)
	}
}

func TestEvaluate_PostSuccessMaintenance(t *testing.T) {
	c := testContract()
	c.PostSuccessPolicy = PostSuccessMaintenance
	evalState := EvalState{}
	var out Outcome
	for day := 0; day <= 30; day++ {
		out, evalState = Evaluate(c, evalState, meetingSample(day), Signal{})
	}
	if out.Status != state.StatusSucceeded || out.ResultCode != state.ResultMissionMaintenanceMode {
		t.Fatalf("Outcome = %+v, want SUCCEEDED/MISSION_MAINTENANCE_MODE", out)
	}
}

func TestLedgerSample_NetMRRUSD(t *testing.T) {
	s := LedgerSample{SubscriptionsUSD: 200, RefundsUSD: 30, CancellationsUSD: 20, DiscountsUSD: 10}
	if got := s.NetMRRUSD(); got != 140 {
		t.Fatalf("NetMRRUSD() = %v, want 140", got)
	}
}

func TestUnimplementedNetMRRSource_ReportsUnavailable(t *testing.T) {
	var src NetMRRSource = UnimplementedNetMRRSource{}
	sample, err := src.Observe(nil, "m1", day0) //nolint:staticcheck // nil context: this stub never uses it
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if sample.Available {
		t.Fatal("Available = true, want false (Task 49 not wired up yet)")
	}
}
