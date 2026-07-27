package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// TestProjector_Idempotency_RealPostgres is the real, SQL-driven proof of
// this task's load-bearing correctness property — idempotent upsert guarded
// by last_seq, offset advanced in the same transaction as the upsert — run
// against an actual Postgres so the ON CONFLICT ... WHERE guard is
// exercised for real, not simulated in Go.
//
// There is no Docker daemon in this task's execution environment (the same
// established blocker as Tasks 2/4/8/12/13), so this test is skipped unless
// PROJECTION_TEST_PG_DSN is set. It is ready to run as-is against any
// reachable Postgres with internal/db/migrations/00002_transitions.sql and
// internal/db/migrations/00003_projection.sql applied, e.g.:
//
//	createdb foundry_projection_test
//	psql foundry_projection_test -f internal/db/migrations/00002_transitions.sql
//	psql foundry_projection_test -f internal/db/migrations/00003_projection.sql
//	PROJECTION_TEST_PG_DSN="postgres://.../foundry_projection_test?sslmode=disable" \
//	    go test ./internal/projection/... -run RealPostgres -v
func TestProjector_Idempotency_RealPostgres(t *testing.T) {
	dsn := os.Getenv("PROJECTION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PROJECTION_TEST_PG_DSN not set — skipping; see doc comment for how to run this against a real Postgres")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `TRUNCATE workflow_transitions, workflow_status_projection`); err != nil {
		t.Fatalf("reset tables: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projection_offsets`); err != nil {
		t.Fatalf("reset offsets: %v", err)
	}

	insert := func(workflowID string, status state.Status, phase state.Phase, attempt int) int64 {
		payload, err := json.Marshal(state.Transition{
			WorkflowID: workflowID,
			Status:     status,
			PhaseTo:    phase,
			Attempt:    attempt,
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("marshal transition: %v", err)
		}
		var seq int64
		err = db.QueryRowContext(ctx,
			`INSERT INTO workflow_transitions (workflow_id, payload) VALUES ($1, $2) RETURNING seq`,
			workflowID, payload,
		).Scan(&seq)
		if err != nil {
			t.Fatalf("insert transition: %v", err)
		}
		return seq
	}

	// wf-a: two transitions in order; wf-b: a single transition. Then
	// simulate a duplicate/out-of-order delivery of wf-a's *first*
	// transition arriving again after the second — the projector must
	// treat this as a no-op, never regressing wf-a's projected state.
	insert("wf-a", state.StatusRunning, "acquiring-worktree", 1)
	seqA2 := insert("wf-a", state.StatusRunning, "executing", 1)
	insert("wf-b", state.StatusRunning, "acquiring-worktree", 1)

	p := &Projector{DB: db}
	for {
		n, err := p.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if n == 0 {
			break
		}
	}

	assertProjected := func(workflowID string, wantPhase state.Phase, wantSeq int64) {
		var phase string
		var lastSeq int64
		err := db.QueryRowContext(ctx,
			`SELECT phase, last_seq FROM workflow_status_projection WHERE workflow_id = $1`, workflowID,
		).Scan(&phase, &lastSeq)
		if err != nil {
			t.Fatalf("query projected row for %s: %v", workflowID, err)
		}
		if phase != string(wantPhase) || lastSeq != wantSeq {
			t.Fatalf("%s projected as (phase=%s, last_seq=%d), want (phase=%s, last_seq=%d)",
				workflowID, phase, lastSeq, wantPhase, wantSeq)
		}
	}
	assertProjected("wf-a", "executing", seqA2)
	assertProjected("wf-b", "acquiring-worktree", seqA2+1)

	// Re-deliver wf-a's earlier transition manually (out-of-order/duplicate
	// simulation): craft a payload with an older phase but insert it as a
	// *new* row with a *lower conceptual* seq is impossible since seq is
	// bigserial-assigned on insert, so instead directly re-run Tick after
	// forging an offset rewind — this proves the upsert guard, not just
	// "nothing new arrived".
	if _, err := db.ExecContext(ctx, `UPDATE projection_offsets SET last_seq = 0`); err != nil {
		t.Fatalf("rewind offset: %v", err)
	}
	for {
		n, err := p.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick after offset rewind: %v", err)
		}
		if n == 0 {
			break
		}
	}
	// Even after reprocessing every transition from the start, wf-a's
	// projected row must still reflect the latest (executing) transition,
	// not regress to the earlier (acquiring-worktree) one that is now
	// re-delivered first within the batch.
	assertProjected("wf-a", "executing", seqA2)
	assertProjected("wf-b", "acquiring-worktree", seqA2+1)

	result, err := Rebuild(ctx, db, DefaultProjectorName)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if result.Rows != 2 {
		t.Fatalf("Rebuild rows = %d, want 2", result.Rows)
	}

	result2, err := Rebuild(ctx, db, DefaultProjectorName)
	if err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	if result2.Checksum != result.Checksum {
		t.Fatalf("rebuild checksum not reproducible: first=%s second=%s", result.Checksum, result2.Checksum)
	}
}

// TestProjector_StaleContentAtNewHigherSeq_RealPostgres reproduces the
// exact defect found live by Task 39 (FND-20, M1 exit drill;
// docs/notes/m1-exit-report.md) and fixed by this change: unlike
// TestProjector_Idempotency_RealPostgres above (which rewinds the *offset*
// and reprocesses the same already-applied seq values), this test appends a
// stale transition as a genuinely NEW row at a NEW, higher seq — the case
// upsertProjectionSQL's old last_seq-only guard let through, regressing the
// projected phase backward. This is the same fixture
// test/projection_rebuild_e2e.sh seeds.
func TestProjector_StaleContentAtNewHigherSeq_RealPostgres(t *testing.T) {
	dsn := os.Getenv("PROJECTION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PROJECTION_TEST_PG_DSN not set — skipping; see TestProjector_Idempotency_RealPostgres's doc comment for how to run this against a real Postgres")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `TRUNCATE workflow_transitions, workflow_status_projection`); err != nil {
		t.Fatalf("reset tables: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projection_offsets`); err != nil {
		t.Fatalf("reset offsets: %v", err)
	}

	insertAt := func(workflowID string, phase state.Phase, occurredAt time.Time) {
		payload, err := json.Marshal(state.Transition{
			WorkflowID: workflowID,
			Status:     state.StatusRunning,
			PhaseTo:    phase,
			Attempt:    1,
			OccurredAt: occurredAt,
		})
		if err != nil {
			t.Fatalf("marshal transition: %v", err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO workflow_transitions (workflow_id, payload) VALUES ($1, $2)`,
			workflowID, payload,
		); err != nil {
			t.Fatalf("insert transition: %v", err)
		}
	}

	t0 := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	insertAt("wf-a", "acquiring-worktree", t0)
	insertAt("wf-a", "executing", t0.Add(time.Minute))
	insertAt("wf-b", "done", t0.Add(2*time.Minute))
	// Stale redelivery: same content as the first wf-a transition, appended
	// LAST so it lands at the highest seq, but its occurred_at is the
	// OLDEST of the three. The old last_seq-only guard would let this win
	// and regress wf-a's projected phase back to "acquiring-worktree".
	insertAt("wf-a", "acquiring-worktree", t0)

	p := &Projector{DB: db}
	for {
		n, err := p.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if n == 0 {
			break
		}
	}

	var phase string
	if err := db.QueryRowContext(ctx,
		`SELECT phase FROM workflow_status_projection WHERE workflow_id = 'wf-a'`,
	).Scan(&phase); err != nil {
		t.Fatalf("query projected phase for wf-a: %v", err)
	}
	if phase != "executing" {
		t.Fatalf("wf-a projected phase = %q, want %q (stale higher-seq redelivery regressed the projection)", phase, "executing")
	}
}
