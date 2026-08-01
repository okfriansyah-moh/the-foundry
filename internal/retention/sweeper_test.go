package retention

import (
	"context"
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

func TestSweeper_SweepAndDeleteDeletesExpiredObjectKeys(t *testing.T) {
	registry := Registry{"evidence": {Name: "evidence", TTL: "1h"}}
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	sweeper := Sweeper{Registry: registry, Now: func() time.Time { return now }}
	deleter := &recordingDeleter{}

	expired, err := sweeper.SweepAndDelete(context.Background(), []SweepCandidate{
		{Class: "evidence", Key: "ab/bundle/manifest.json", CreatedAt: now.Add(-2 * time.Hour)},
		{Class: "evidence", Key: "cd/bundle/manifest.json", CreatedAt: now.Add(-30 * time.Minute)},
	}, deleter)
	if err != nil {
		t.Fatalf("SweepAndDelete: %v", err)
	}
	if len(expired) != 1 || expired[0] != "ab/bundle/manifest.json" {
		t.Fatalf("expired=%v want [ab/bundle/manifest.json]", expired)
	}
	if len(deleter.keys) != 1 || deleter.keys[0] != "ab/bundle/manifest.json" {
		t.Fatalf("deleted=%v want [ab/bundle/manifest.json]", deleter.keys)
	}
}

func TestSweeper_SweepAndDeleteHonorsLegalHolds(t *testing.T) {
	registry := Registry{"evidence": {Name: "evidence", TTL: "1h"}}
	holds := NewHoldRegistry()
	holds.Hold("ab/bundle/manifest.json", "legal")
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	sweeper := Sweeper{Registry: registry, Holds: holds, Now: func() time.Time { return now }}
	deleter := &recordingDeleter{}

	expired, err := sweeper.SweepAndDelete(context.Background(), []SweepCandidate{
		{Class: "evidence", Key: "ab/bundle/manifest.json", CreatedAt: now.Add(-2 * time.Hour)},
	}, deleter)
	if err != nil {
		t.Fatalf("SweepAndDelete: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("expired=%v want none", expired)
	}
	if len(deleter.keys) != 0 {
		t.Fatalf("deleted=%v want none", deleter.keys)
	}
}

type recordingDeleter struct {
	keys []string
}

func (d *recordingDeleter) DeleteKey(_ context.Context, key string) error {
	d.keys = append(d.keys, key)
	return nil
}
