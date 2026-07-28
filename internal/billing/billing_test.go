package billing

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestWebhookReplayIdempotent(t *testing.T) {
	store := NewEventStore()
	if !store.Handle("evt_1") {
		t.Fatal("first webhook should be accepted")
	}
	if store.Handle("evt_1") {
		t.Fatal("replayed webhook should be ignored")
	}
	if got := store.Total(); got != 1 {
		t.Fatalf("total=%d, want 1", got)
	}
}

func TestReconcile_TestClockScenarioMatchesCent(t *testing.T) {
	row := Reconcile(ReconciliationInput{
		SubscriptionsUSD: 300.00, // 3 subs
		RefundsUSD:       100.00, // 1 refund
		CancellationsUSD: 0,
		DiscountsUSD:     0,
		At:               time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	want := 200.00
	if math.Abs(row.NetMRRUSD-want) > 0.0001 {
		t.Fatalf("net mrr=%.4f, want %.4f", row.NetMRRUSD, want)
	}
}

func TestUnavailableProviderMapsToMissionPauseSignal(t *testing.T) {
	src := MissionNetMRRSource{Available: false}
	sample, err := src.Observe(context.Background(), "m1", time.Now().UTC())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if sample.Available {
		t.Fatal("expected unavailable sample when provider is unreachable")
	}
}
