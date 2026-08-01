package mission_test

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// TestMissionActivityIdempotency proves docs/PLAN.md Task 122's core guarantee:
// each of the four state-mutating mission activities produces EXACTLY ONE row
// under a retry, and — for the deterministic-id activities — even under a
// commit-then-crash-then-retry sequence where the receipt was never written.
func TestMissionActivityIdempotency(t *testing.T) {
	db := openTestDB(t) // skips without a DSN
	ctx := context.Background()
	store := mission.NewStore(db)
	suffix := randSuffix(t)
	principalID := "principal-" + suffix
	insertTestPrincipal(t, db, principalID)
	mid := "mission-" + suffix
	wf := "mission-wf-" + suffix
	if err := store.CreateMission(ctx, mission.Mission{ID: mid, PrincipalID: principalID, WorkflowID: wf, Contract: minimalContract(suffix)}); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	countStates := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mission_state WHERE mission_id = $1`, mid).Scan(&n); err != nil {
			t.Fatalf("count mission_state: %v", err)
		}
		return n
	}
	countGates := func() int {
		evs, err := store.ListGateEvents(ctx, mid)
		if err != nil {
			t.Fatalf("list gate events: %v", err)
		}
		return len(evs)
	}

	// --- RecordMissionState: ordinary retry (same receipt store) ---
	acts := mission.NewActivities(store, store, store, store, store, kernel.NewMemTransitionStore(), nil, mission.UnimplementedNetMRRSource{}, kernel.NewMemReceiptStore())
	in := missionStateInputFor(wf, mid, 7)
	if err := acts.RecordMissionState(ctx, in); err != nil {
		t.Fatalf("record state 1: %v", err)
	}
	if err := acts.RecordMissionState(ctx, in); err != nil {
		t.Fatalf("record state 2 (retry): %v", err)
	}
	if n := countStates(); n != 1 {
		t.Fatalf("mission_state rows after retry = %d, want 1", n)
	}

	// --- RecordMissionState: commit-then-crash-then-retry (receipt lost) ---
	// A fresh Activities with an empty receipt store models a crash after the
	// Postgres commit but before the receipt write: fn re-runs, and the
	// deterministic id + ON CONFLICT DO NOTHING still leaves exactly one row.
	acts2 := mission.NewActivities(store, store, store, store, store, kernel.NewMemTransitionStore(), nil, mission.UnimplementedNetMRRSource{}, kernel.NewMemReceiptStore())
	if err := acts2.RecordMissionState(ctx, in); err != nil {
		t.Fatalf("record state 3 (post-crash retry): %v", err)
	}
	if n := countStates(); n != 1 {
		t.Fatalf("mission_state rows after post-crash retry = %d, want 1", n)
	}

	// --- RecordGateEvent: retry addresses the same row, same id ---
	gin := gateEventInputFor(wf, mid, 3)
	id1, err := acts.RecordGateEvent(ctx, gin)
	if err != nil {
		t.Fatalf("record gate 1: %v", err)
	}
	id2, err := acts.RecordGateEvent(ctx, gin)
	if err != nil {
		t.Fatalf("record gate 2 (retry): %v", err)
	}
	if id1 != id2 {
		t.Fatalf("gate id not stable across retry: %q vs %q", id1, id2)
	}
	// Post-crash retry with a fresh receipt store: still one gate row.
	if _, err := acts2.RecordGateEvent(ctx, gin); err != nil {
		t.Fatalf("record gate 3 (post-crash retry): %v", err)
	}
	if n := countGates(); n != 1 {
		t.Fatalf("gate_events rows after retries = %d, want 1", n)
	}

	// --- ResolveGateEvent addresses exactly the recorded gate row ---
	if err := acts.ResolveGateEvent(ctx, resolveGateInputFor(wf, id1, 3)); err != nil {
		t.Fatalf("resolve gate: %v", err)
	}
	evs, err := store.ListGateEvents(ctx, mid)
	if err != nil {
		t.Fatalf("list gate events: %v", err)
	}
	if len(evs) != 1 || evs[0].ResolvedAt == nil {
		t.Fatalf("gate not resolved exactly once: %+v", evs)
	}
}

func missionStateInputFor(wf, mid string, iteration int) mission.MissionStateInput {
	return mission.MissionStateInput{
		WorkflowID:    wf,
		LoopIteration: iteration,
		MissionID:     mid,
		At:            time.Unix(1_700_000_000, 0).UTC(),
	}
}

func gateEventInputFor(wf, mid string, iteration int) mission.GateEventInput {
	return mission.GateEventInput{
		WorkflowID:    wf,
		LoopIteration: iteration,
		MissionID:     mid,
		Action:        "unforeseen-human-gate",
		OccurredAt:    time.Unix(1_700_000_100, 0).UTC(),
	}
}

func resolveGateInputFor(wf, gateID string, iteration int) mission.ResolveGateInput {
	return mission.ResolveGateInput{
		WorkflowID:    wf,
		LoopIteration: iteration,
		GateEventID:   gateID,
		Resolution:    "resumed",
		ResolvedAt:    time.Unix(1_700_000_200, 0).UTC(),
	}
}
