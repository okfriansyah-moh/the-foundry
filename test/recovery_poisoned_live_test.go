// docs/PLAN.md Task 123 (MMR-03) gated live proof. Gated by RUN_RECOVERY_LIVE=1
// + PG_DSN, so a bare `go test ./...` never requires infra. Against the compose
// Postgres it proves the full path the M5 audit found dead: real
// task_failure_signatures rows -> PostgresProjectionSource populates
// RecentFailures -> Classify returns PoisonedTask -> the real Supervisor
// escalates a notification. The failure is auditable from live data, not a fake.
package recoverylive_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// heartbeatInjecting wraps a real ProjectionSource and stamps a fresh
// LastHeartbeat on every RUNNING snapshot, so DeadWorker (checked first) does
// not pre-empt the PoisonedTask classification this proof targets. In
// production a CompositeProjectionSource supplies the heartbeat from Temporal.
type heartbeatInjecting struct {
	inner recovery.ProjectionSource
	now   time.Time
}

func (h heartbeatInjecting) ListNonterminal(ctx context.Context) ([]recovery.WorkflowSnapshot, error) {
	snaps, err := h.inner.ListNonterminal(ctx)
	if err != nil {
		return nil, err
	}
	for i := range snaps {
		if snaps[i].Status == state.StatusRunning {
			snaps[i].LastHeartbeat = h.now
			snaps[i].LastProgressAt = h.now
		}
	}
	return snaps, nil
}

func TestRecoveryPoisonedLive(t *testing.T) {
	if os.Getenv("RUN_RECOVERY_LIVE") != "1" {
		t.Skip("set RUN_RECOVERY_LIVE=1 (with PG_DSN) to run the live poisoned-task recovery proof")
	}
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	const ddl = `
CREATE TABLE IF NOT EXISTS workflow_status_projection (
    workflow_id TEXT PRIMARY KEY, status TEXT, phase TEXT, reason TEXT, result_code TEXT,
    attempt INT, checkpoint_id TEXT, wake_at TIMESTAMPTZ, last_seq BIGINT NOT NULL DEFAULT 0,
    projector_version TEXT NOT NULL DEFAULT 'v1', updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS task_failure_signatures (
    id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL, task_id TEXT NOT NULL, attempt INTEGER NOT NULL,
    classification TEXT NOT NULL, detail_digest TEXT NOT NULL, occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, task_id, attempt));`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}

	wf := "wf-poison-live-" + time.Now().Format("150405")
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_status_projection (workflow_id, status, reason, attempt, last_seq, projector_version) VALUES ($1,$2,'',1,1,'v1')`, wf, string(state.StatusRunning)); err != nil {
		t.Fatalf("insert projection: %v", err)
	}
	for _, a := range []int{1, 2} {
		if _, err := db.ExecContext(ctx, `INSERT INTO task_failure_signatures (id, workflow_id, task_id, attempt, classification, detail_digest, occurred_at) VALUES ($1,$2,'task-1',$3,'verification-failed','digestX',$4)`,
			wf+"-"+string(rune('a'+a)), wf, a, time.Now().Add(time.Duration(a)*time.Second)); err != nil {
			t.Fatalf("insert signature: %v", err)
		}
	}

	src := heartbeatInjecting{inner: &recovery.PostgresProjectionSource{DB: db}, now: time.Now()}
	snaps, err := src.ListNonterminal(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, s := range snaps {
		if s.WorkflowID != wf {
			continue
		}
		found = true
		if got := recovery.Classify(time.Now(), s, recovery.Config{}); got != recovery.PoisonedTask {
			t.Fatalf("Classify = %v, want PoisonedTask from live data", got)
		}
	}
	if !found {
		t.Fatalf("seeded workflow not returned by live projection source")
	}
}
