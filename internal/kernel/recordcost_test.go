package kernel_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

func newRecordCostActivities(store *kernel.MemBudgetStore, rates cost.RateTable) *kernel.Activities {
	a := kernel.NewActivities(nil, nil, nil, nil, kernel.NewMemReceiptStore(), nil, store, cost.Defaults{}, verify.Runner{})
	a.ModelRates = rates
	return a
}

// TestRecordCost_ProviderReportedIncursActual proves Task 120: a completed task
// incurs its real, provider-reported cost against its reservation.
func TestRecordCost_ProviderReportedIncursActual(t *testing.T) {
	store := kernel.NewMemBudgetStore()
	a := newRecordCostActivities(store, cost.NewRateTable())
	out, err := a.RecordCost(context.Background(), kernel.RecordCostInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1, EntryID: "entry-1", ExecutorName: "api",
		Usage: executor.Usage{ProviderReportedUSD: 0.42, Model: "gpt-4o"},
	})
	if err != nil {
		t.Fatalf("RecordCost: %v", err)
	}
	if out.Unknown || out.IncurredUSD != 0.42 {
		t.Fatalf("want incurred 0.42, got %+v", out)
	}
	if got, ok := store.IncurredFor("entry-1"); !ok || got != 0.42 {
		t.Fatalf("reservation must be incurred with the actual: got %v ok=%v", got, ok)
	}
}

// TestRecordCost_TokensPricedFromRateTable proves token counts become dollars
// deterministically when no provider dollar figure is reported.
func TestRecordCost_TokensPricedFromRateTable(t *testing.T) {
	rates := cost.NewRateTable(cost.ModelRate{Model: "gpt-4o", InputPer1KUSD: 0.0025, OutputPer1KUSD: 0.010})
	store := kernel.NewMemBudgetStore()
	a := newRecordCostActivities(store, rates)
	out, err := a.RecordCost(context.Background(), kernel.RecordCostInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1, EntryID: "entry-1",
		Usage: executor.Usage{InputTokens: 1000, OutputTokens: 500, Model: "gpt-4o"},
	})
	if err != nil {
		t.Fatalf("RecordCost: %v", err)
	}
	// 1000/1000*0.0025 + 500/1000*0.010 = 0.0025 + 0.005 = 0.0075
	if out.Unknown || (out.IncurredUSD < 0.00749 || out.IncurredUSD > 0.00751) {
		t.Fatalf("want ~0.0075 priced from tokens, got %+v", out)
	}
}

// TestRecordCost_UnknownModelRecordsUnknownNotDefault proves the refusal to
// fabricate: a model with no rate records unknown, never a global default.
func TestRecordCost_UnknownModelRecordsUnknownNotDefault(t *testing.T) {
	store := kernel.NewMemBudgetStore()
	a := newRecordCostActivities(store, cost.NewRateTable()) // empty rate table
	out, err := a.RecordCost(context.Background(), kernel.RecordCostInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1, EntryID: "entry-1",
		Usage: executor.Usage{InputTokens: 100, OutputTokens: 50, Model: "mystery-model"},
	})
	if err != nil {
		t.Fatalf("RecordCost: %v", err)
	}
	if !out.Unknown {
		t.Fatalf("an unrated model must record unknown, got %+v", out)
	}
	if _, ok := store.IncurredFor("entry-1"); ok {
		t.Fatal("unknown cost must not incur a fabricated figure")
	}
}

// TestRecordCost_IdempotentPerAttempt proves the receipt guard prevents a
// double-incur on retry.
func TestRecordCost_IdempotentPerAttempt(t *testing.T) {
	store := kernel.NewMemBudgetStore()
	a := newRecordCostActivities(store, cost.NewRateTable())
	in := kernel.RecordCostInput{WorkflowID: "wf1", TaskID: "t1", Attempt: 1, EntryID: "entry-1",
		Usage: executor.Usage{ProviderReportedUSD: 1.0, Model: "gpt-4o"}}
	first, _ := a.RecordCost(context.Background(), in)
	second, _ := a.RecordCost(context.Background(), in)
	if first.IncurredUSD != second.IncurredUSD {
		t.Fatalf("retry must return the same receipt: %v vs %v", first, second)
	}
}
