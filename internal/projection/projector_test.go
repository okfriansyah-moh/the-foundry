package projection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func TestDecodeTransition(t *testing.T) {
	occurred := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	in := state.Transition{
		WorkflowID: "wf-1",
		Status:     state.StatusRunning,
		PhaseTo:    "executing",
		Attempt:    2,
		OccurredAt: occurred,
	}
	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	got, err := decodeTransition(payload)
	if err != nil {
		t.Fatalf("decodeTransition: %v", err)
	}
	if got.WorkflowID != in.WorkflowID || got.Status != in.Status || got.PhaseTo != in.PhaseTo || got.Attempt != in.Attempt {
		t.Fatalf("decodeTransition round-trip mismatch: got %+v, want %+v", got, in)
	}
	if !got.OccurredAt.Equal(occurred) {
		t.Fatalf("OccurredAt = %v, want %v", got.OccurredAt, occurred)
	}
}

func TestDecodeTransition_InvalidJSON(t *testing.T) {
	if _, err := decodeTransition([]byte("not json")); err == nil {
		t.Fatal("expected error decoding invalid JSON payload")
	}
}

func TestProjector_NameAndBatchSizeDefaults(t *testing.T) {
	p := &Projector{}
	if got := p.name(); got != DefaultProjectorName {
		t.Fatalf("name() = %q, want %q", got, DefaultProjectorName)
	}
	if got := p.batchSize(); got != DefaultBatchSize {
		t.Fatalf("batchSize() = %d, want %d", got, DefaultBatchSize)
	}

	p2 := &Projector{Name: "custom", BatchSize: 7}
	if got := p2.name(); got != "custom" {
		t.Fatalf("name() = %q, want %q", got, "custom")
	}
	if got := p2.batchSize(); got != 7 {
		t.Fatalf("batchSize() = %d, want %d", got, 7)
	}
}

// TestProjector_Run_StopsOnContextCancel proves Run's loop exits promptly
// via ctx cancellation without ever needing a live DB (Tick is never
// reached because the interval is far longer than the test timeout and ctx
// is already cancelled when Run is called).
func TestProjector_Run_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &Projector{} // DB is nil; Tick must never be invoked
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx, time.Hour) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Run to return ctx.Err(), got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after ctx cancellation")
	}
}

// TestUpsertProjectionSQL_HasIdempotencyGuard pins the load-bearing
// correctness property described in docs/PLAN.md Task 14 Step 2 — the
// ON CONFLICT DO UPDATE is guarded by a WHERE clause comparing the
// semantically-ordered (occurred_at, last_seq) tuple, so out-of-order and
// duplicate delivery (including a stale transition redelivered at a new,
// higher seq — the bug found live by Task 39/FND-20, fixed here) is a
// no-op, never a regression. A full exercise of this guard against real
// Postgres semantics lives in projector_pg_test.go (gated on
// PROJECTION_TEST_PG_DSN); this test just guards against someone silently
// dropping or weakening the WHERE clause in a refactor.
func TestUpsertProjectionSQL_HasIdempotencyGuard(t *testing.T) {
	const want = "WHERE (EXCLUDED.occurred_at, EXCLUDED.last_seq) > (COALESCE(workflow_status_projection.occurred_at, '-infinity'), workflow_status_projection.last_seq)"
	if !strings.Contains(upsertProjectionSQL, want) {
		t.Fatalf("upsertProjectionSQL missing idempotency guard %q:\n%s", want, upsertProjectionSQL)
	}
}
