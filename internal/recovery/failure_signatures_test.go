package recovery_test

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// TestListNonterminal_PopulatesRecentFailures proves docs/PLAN.md Task 123's
// core plumbing: the failure-signature history the kernel writes is read back
// into WorkflowSnapshot.RecentFailures, and two identical failures classify as
// PoisonedTask (the condition that was undetectable while RecentFailures was
// nil), while distinct failures do not.
func TestListNonterminal_PopulatesRecentFailures(t *testing.T) {
	db := openTestDB(t) // skips without a DSN; truncates the projection table
	ctx := context.Background()
	const ddl = `
CREATE TABLE IF NOT EXISTS task_failure_signatures (
    id             TEXT PRIMARY KEY,
    workflow_id    TEXT NOT NULL,
    task_id        TEXT NOT NULL,
    attempt        INTEGER NOT NULL,
    classification TEXT NOT NULL,
    detail_digest  TEXT NOT NULL,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, task_id, attempt)
)`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("create task_failure_signatures: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM task_failure_signatures`); err != nil {
		t.Fatalf("truncate task_failure_signatures: %v", err)
	}

	// A RUNNING workflow with two IDENTICAL failure signatures for its task.
	poisoned := "wf-poison"
	if _, err := db.ExecContext(ctx, `
INSERT INTO workflow_status_projection (workflow_id, status, reason, attempt, last_seq, projector_version, updated_at)
VALUES ($1, $2, '', 1, 1, 'v1', now())`, poisoned, string(state.StatusRunning)); err != nil {
		t.Fatalf("insert poisoned projection: %v", err)
	}
	insertSig := func(wf, task string, attempt int, class, digest string, at time.Time) {
		if _, err := db.ExecContext(ctx, `
INSERT INTO task_failure_signatures (id, workflow_id, task_id, attempt, classification, detail_digest, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			wf+"-"+task+"-"+time.Now().Format("150405.000000")+string(rune('a'+attempt)), wf, task, attempt, class, digest, at); err != nil {
			t.Fatalf("insert signature: %v", err)
		}
	}
	base := time.Now().Add(-time.Minute)
	insertSig(poisoned, "task-1", 1, "verification-failed", "digestX", base)
	insertSig(poisoned, "task-1", 2, "verification-failed", "digestX", base.Add(time.Second))

	src := &recovery.PostgresProjectionSource{DB: db}
	snaps, err := src.ListNonterminal(ctx)
	if err != nil {
		t.Fatalf("ListNonterminal: %v", err)
	}
	var got *recovery.WorkflowSnapshot
	for i := range snaps {
		if snaps[i].WorkflowID == poisoned {
			got = &snaps[i]
		}
	}
	if got == nil {
		t.Fatalf("poisoned workflow not returned")
	}
	if len(got.RecentFailures) != 2 {
		t.Fatalf("RecentFailures = %d, want 2 (populated from task_failure_signatures)", len(got.RecentFailures))
	}
	if got.RecentFailures[0].Key() != got.RecentFailures[1].Key() {
		t.Fatalf("expected two identical signatures, got %q vs %q", got.RecentFailures[0].Key(), got.RecentFailures[1].Key())
	}

	// With a fresh heartbeat (so DeadWorker does not pre-empt), identical
	// failures classify as PoisonedTask — against LIVE data, not a fake.
	got.LastHeartbeat = time.Now()
	got.LastProgressAt = time.Now()
	if c := recovery.Classify(time.Now(), *got, recovery.Config{}); c != recovery.PoisonedTask {
		t.Fatalf("Classify = %v, want PoisonedTask", c)
	}

	// A workflow whose two failures are DISTINCT is not PoisonedTask.
	distinct := "wf-distinct"
	if _, err := db.ExecContext(ctx, `
INSERT INTO workflow_status_projection (workflow_id, status, reason, attempt, last_seq, projector_version, updated_at)
VALUES ($1, $2, '', 1, 1, 'v1', now())`, distinct, string(state.StatusRunning)); err != nil {
		t.Fatalf("insert distinct projection: %v", err)
	}
	insertSig(distinct, "task-1", 1, "verification-failed", "digestA", base)
	insertSig(distinct, "task-1", 2, "verification-failed", "digestB", base.Add(time.Second))
	snaps, err = src.ListNonterminal(ctx)
	if err != nil {
		t.Fatalf("ListNonterminal 2: %v", err)
	}
	for i := range snaps {
		if snaps[i].WorkflowID == distinct {
			s := snaps[i]
			s.LastHeartbeat = time.Now()
			s.LastProgressAt = time.Now()
			if c := recovery.Classify(time.Now(), s, recovery.Config{}); c == recovery.PoisonedTask {
				t.Fatalf("distinct-signature workflow misclassified as PoisonedTask")
			}
		}
	}
}
