package kernel_test

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// newReserveBudgetTestActivities builds a minimal Activities set exercising
// only ReserveBudget's own collaborators (CostStore, CostDefaults,
// ReceiptStore) — ReserveBudget never touches provenance, worktree,
// evidence, lease, or transitions.
func newReserveBudgetTestActivities(budgetStore *kernel.MemBudgetStore, defaultUSD float64) *kernel.Activities {
	return kernel.NewActivities(nil, nil, nil, nil, kernel.NewMemReceiptStore(), nil, budgetStore, cost.Defaults{DefaultUSD: defaultUSD}, verify.Runner{})
}

// TestReserveBudget_AttendedUnmeteredWithoutEnvelope: an ATTENDED reservation
// with no envelope stays unmetered (a human is present) — Task 119 preserves
// interactive use.
func TestReserveBudget_AttendedUnmeteredWithoutEnvelope(t *testing.T) {
	acts := newReserveBudgetTestActivities(kernel.NewMemBudgetStore(), 0.10)

	out, err := acts.ReserveBudget(context.Background(), kernel.ReserveBudgetInput{
		WorkflowID: "wf1", TaskID: "t1", ExecutorName: "fake", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("reserve budget: %v", err)
	}
	if out.Exhausted || out.Shadow || out.Refused {
		t.Fatalf("out = %+v, want unmetered (attended, no envelope)", out)
	}
}

// TestReserveBudget_UnattendedRefusesWithoutEnvelope proves Task 119 (COST-01):
// an UNATTENDED mission with no budget envelope is REFUSED, not run unmetered.
func TestReserveBudget_UnattendedRefusesWithoutEnvelope(t *testing.T) {
	acts := newReserveBudgetTestActivities(kernel.NewMemBudgetStore(), 0.10)

	out, err := acts.ReserveBudget(context.Background(), kernel.ReserveBudgetInput{
		WorkflowID: "wf1", TaskID: "t1", ExecutorName: "fake", Attempt: 1,
		MissionID: "m1", Unattended: true,
	})
	if err != nil {
		t.Fatalf("reserve budget: %v", err)
	}
	if !out.Refused {
		t.Fatalf("out = %+v, want Refused=true (unattended, no envelope must fail closed)", out)
	}
	if out.Classification != "budget-envelope-absent" {
		t.Fatalf("want classification budget-envelope-absent, got %q", out.Classification)
	}
}

// TestReserveBudget_ExhaustedEnvelopeCancelsWithoutError proves
// ReserveBudget reports exhaustion as data (ReserveBudgetOutput.Exhausted),
// not a Go error — the distinction DeliverPlan's runTask depends on to
// route to WAITING/budget instead of a genuine activity failure.
// MemBudgetStore's own ceiling arithmetic is covered directly by
// budget_test.go's TestMemBudgetStore_ExhaustsAtCeiling; this test isolates
// ReserveBudget's error-vs-data translation using a stub BudgetStore whose
// Reserve always reports exhaustion (currentPeriod is unexported, so a
// real MemBudgetStore here would need this test to duplicate that
// calendar-month computation just to seed the right key).
func TestReserveBudget_ExhaustedEnvelopeCancelsWithoutError(t *testing.T) {
	acts := kernel.NewActivities(nil, nil, nil, nil, kernel.NewMemReceiptStore(), nil, &fakeExhaustedStore{}, cost.Defaults{DefaultUSD: 0.10}, verify.Runner{})

	out, err := acts.ReserveBudget(context.Background(), kernel.ReserveBudgetInput{
		WorkflowID: "wf1", TaskID: "t1", ExecutorName: "fake", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("reserve budget: %v (exhaustion must be reported as data, not a Go error)", err)
	}
	if !out.Exhausted {
		t.Fatalf("out = %+v, want Exhausted=true", out)
	}
}

func TestReserveBudget_SubscriptionExecutorTakesShadowPath(t *testing.T) {
	budgetStore := kernel.NewMemBudgetStore()
	// Deliberately no ceiling provisioned — if the shadow path incorrectly
	// fell through to Reserve, this would return ErrBudgetNotFound instead
	// of the shadow entry, and the assertions below would catch it.
	acts := newReserveBudgetTestActivities(budgetStore, 0.10)

	out, err := acts.ReserveBudget(context.Background(), kernel.ReserveBudgetInput{
		WorkflowID: "wf1", TaskID: "t1", ExecutorName: "claudecode", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("reserve budget: %v", err)
	}
	if !out.Shadow || out.Exhausted {
		t.Fatalf("out = %+v, want Shadow=true, Exhausted=false", out)
	}
	if out.EntryID == "" {
		t.Fatal("EntryID is empty, want a recorded shadow entry id")
	}
}

func TestReserveBudget_IdempotentPerAttempt(t *testing.T) {
	budgetStore := kernel.NewMemBudgetStore()
	// period must match what ReserveBudget's unexported currentPeriod
	// actually computes for "now" — the real calendar-month bucket.
	budgetStore.SetCeiling(cost.ScopeWorkflow, "wf1", cost.KindMissionMonthly, time.Now().Format("2006-01"), 100)
	acts := newReserveBudgetTestActivities(budgetStore, 0.10)

	in := kernel.ReserveBudgetInput{WorkflowID: "wf1", TaskID: "t1", ExecutorName: "fake", Attempt: 1}
	first, err := acts.ReserveBudget(context.Background(), in)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	second, err := acts.ReserveBudget(context.Background(), in)
	if err != nil {
		t.Fatalf("second reserve (same attempt): %v", err)
	}
	if first != second {
		t.Fatalf("first = %+v, second = %+v; same Attempt must return the cached receipt, not reserve twice", first, second)
	}

	// A different Attempt must genuinely re-invoke Reserve.
	in.Attempt = 2
	third, err := acts.ReserveBudget(context.Background(), in)
	if err != nil {
		t.Fatalf("third reserve (new attempt): %v", err)
	}
	if third.EntryID == first.EntryID {
		t.Fatalf("third.EntryID = %q, want different from first %q (new attempt must reserve again)", third.EntryID, first.EntryID)
	}
}

// fakeExhaustedStore is a kernel.BudgetStore stub whose Reserve always
// reports the envelope exhausted, isolating ReserveBudget's own
// Exhausted-reporting behavior from MemBudgetStore's ceiling arithmetic
// (already covered by budget_test.go).
type fakeExhaustedStore struct{}

func (f *fakeExhaustedStore) Reserve(_ context.Context, _ cost.Scope, _ string, _ cost.Kind, _ string, _ float64, _, _ string, _ any) (cost.Entry, error) {
	return cost.Entry{}, cost.ErrBudgetExhausted
}

func (f *fakeExhaustedStore) RecordShadow(_ context.Context, scope cost.Scope, scopeID string, amountUSD float64, provider, pricingVersion string, _ any) (cost.Entry, error) {
	return cost.Entry{ID: "shadow-1", Scope: scope, ScopeID: scopeID, State: cost.StateShadow, AmountUSD: amountUSD, Provider: provider, PricingVersion: pricingVersion}, nil
}

func (f *fakeExhaustedStore) Incur(_ context.Context, entryID string, actualAmountUSD float64) (cost.Entry, error) {
	return cost.Entry{ID: entryID, State: cost.StateIncurred, AmountUSD: actualAmountUSD}, nil
}
