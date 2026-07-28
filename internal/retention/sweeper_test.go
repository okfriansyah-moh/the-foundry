package retention

import (
	"testing"
	"time"
)

func TestSweeper_HoldBlocksDeletion(t *testing.T) {
	registry := Registry{"customer_data": {Name: "customer_data", TTL: "1h"}}
	holds := NewHoldRegistry()
	holds.Hold("user-1", "legal")
	sweeper := Sweeper{Registry: registry, Holds: holds, Now: func() time.Time { return time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC) }}
	expired, err := sweeper.Sweep([]SweepCandidate{{Class: "customer_data", Key: "user-1", CreatedAt: time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("expired=%v want none", expired)
	}
}
