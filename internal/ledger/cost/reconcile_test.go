package cost

import "testing"

func TestReconcileEntry_ReleaseAndVariance(t *testing.T) {
	result, err := ReconcileEntry(Entry{ID: "e1", State: StateReserved, AmountUSD: 10}, 0, 1)
	if err != nil {
		t.Fatalf("ReconcileEntry: %v", err)
	}
	if !result.Released {
		t.Fatal("expected zero-observed reserved entry to be releasable")
	}
}
