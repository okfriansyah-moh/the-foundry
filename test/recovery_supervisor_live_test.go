// Package recoverylive_test is docs/PLAN.md Task 94 (FND-13R)'s live
// proof: it runs internal/recovery.Supervisor.ScanOnce for real, against
// this repo's own docker-compose Postgres+Temporal, and asserts a real
// repair (DeadWorker) and a real escalation (MissingWake) — the two
// conditions live Postgres+Temporal data can support today (see Task
// 94's own Out-of-scope note on PoisonedTask/InfiniteRetry).
//
// Gated behind RUN_RECOVERY_LIVE=1 (mirrors internal/scm/write's
// RUN_GITHUB=1 gated-live-test precedent) because it needs a real
// PG_DSN + TEMPORAL_HOSTPORT — never available by default in a bare
// `go test ./...` run outside `make test`'s Docker environment.
package recoverylive_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// stuckWorkflow blocks forever on a signal nobody ever sends — a
// minimal, self-contained stand-in for a real DeliverPlan execution
// whose worker died: Temporal reports it RUNNING indefinitely, with no
// activity ever scheduled (so TemporalHeartbeatSource falls back to
// StartTime, which only gets staler as the test sleeps past a tiny
// StaleAfter).
func stuckWorkflow(ctx workflow.Context) error {
	workflow.GetSignalChannel(ctx, "never-sent").Receive(ctx, nil)
	return nil
}

func liveEnv(t *testing.T) (pgDSN, temporalHostPort string) {
	t.Helper()
	if os.Getenv("RUN_RECOVERY_LIVE") != "1" {
		t.Skip("RUN_RECOVERY_LIVE=1 not set — this gated test needs a real docker-compose Postgres+Temporal; run via `docker compose run --rm dev` with RUN_RECOVERY_LIVE=1 PG_DSN=... TEMPORAL_HOSTPORT=... set")
	}
	pgDSN = os.Getenv("PG_DSN")
	temporalHostPort = os.Getenv("TEMPORAL_HOSTPORT")
	if pgDSN == "" || temporalHostPort == "" {
		t.Fatal("RUN_RECOVERY_LIVE=1 requires PG_DSN and TEMPORAL_HOSTPORT")
	}
	return pgDSN, temporalHostPort
}

// ensureProjectionTable mirrors internal/db/migrations/00003_projection.sql
// and internal/db/migrations/00007_notifications.sql — inlined (rather
// than run via the real migration tool) so this test works whether or
// not `foundry migrate up` has been run against pgDSN yet, matching
// internal/notify/store_test.go's and internal/recovery/postgres_test.go's
// own precedent. Never truncates — this is a shared dev database other
// concurrent sessions may be using — only ever touches rows this test's
// own unique workflow IDs own.
func ensureLiveTables(t *testing.T, db *sql.DB) {
	t.Helper()
	const projection = `
CREATE TABLE IF NOT EXISTS workflow_status_projection (
    workflow_id       TEXT PRIMARY KEY,
    status            TEXT,
    phase             TEXT,
    reason            TEXT,
    result_code       TEXT,
    attempt           INT,
    checkpoint_id     TEXT,
    wake_at           TIMESTAMPTZ,
    last_seq          BIGINT NOT NULL,
    projector_version TEXT NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	if _, err := db.Exec(projection); err != nil {
		t.Fatalf("create workflow_status_projection: %v", err)
	}

	const notifications = `
CREATE TABLE IF NOT EXISTS notifications (
    id         TEXT PRIMARY KEY,
    channel    TEXT NOT NULL,
    target     TEXT NOT NULL,
    class      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('pending', 'sent', 'failed', 'acked')) DEFAULT 'pending',
    attempts   INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ
)`
	if _, err := db.Exec(notifications); err != nil {
		t.Fatalf("create notifications: %v", err)
	}
}

func insertProjectionRow(t *testing.T, db *sql.DB, workflowID string, status state.Status, reason state.Reason, wakeAt *time.Time) {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO workflow_status_projection (workflow_id, status, reason, attempt, wake_at, last_seq, projector_version)
VALUES ($1, $2, $3, $4, $5, 1, 'v1')
ON CONFLICT (workflow_id) DO UPDATE SET status = EXCLUDED.status, reason = EXCLUDED.reason, wake_at = EXCLUDED.wake_at, updated_at = now()`,
		workflowID, string(status), string(reason), 0, wakeAt)
	if err != nil {
		t.Fatalf("insert projection row for %s: %v", workflowID, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workflow_status_projection WHERE workflow_id = $1`, workflowID)
	})
}

func resultFor(results []recovery.ScanResult, workflowID string) (recovery.ScanResult, bool) {
	for _, r := range results {
		if r.WorkflowID == workflowID {
			return r, true
		}
	}
	return recovery.ScanResult{}, false
}

// TestRecoverySupervisorLive_DeadWorkerIsRepaired manufactures a real
// Temporal workflow execution that is RUNNING but will never progress
// (blocked on a signal nobody sends), gives it a matching
// workflow_status_projection row, and asserts one real
// Supervisor.ScanOnce classifies it DeadWorker (via a real
// TemporalHeartbeatSource.Heartbeat falling back to StartTime) and
// repairs it via a real TemporalController.Reset — proven by a changed
// RunID, not merely a nil error.
func TestRecoverySupervisorLive_DeadWorkerIsRepaired(t *testing.T) {
	pgDSN, temporalHostPort := liveEnv(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ensureLiveTables(t, db)

	c, err := client.Dial(client.Options{HostPort: temporalHostPort})
	if err != nil {
		t.Fatalf("dial temporal: %v", err)
	}
	t.Cleanup(c.Close)

	taskQueue := fmt.Sprintf("recovery-live-%d", time.Now().UnixNano())
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(stuckWorkflow)
	if err := w.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	t.Cleanup(w.Stop)

	workflowID := fmt.Sprintf("recovery-live-deadworker-%d", time.Now().UnixNano())
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: taskQueue}, stuckWorkflow)
	if err != nil {
		t.Fatalf("start stuck workflow: %v", err)
	}
	originalRunID := run.GetRunID()
	t.Cleanup(func() { _ = c.TerminateWorkflow(context.Background(), workflowID, "", "recovery live test cleanup") })

	insertProjectionRow(t, db, workflowID, state.StatusRunning, "", nil)

	sup := &recovery.Supervisor{
		Source: &recovery.CompositeProjectionSource{
			PG:         &recovery.PostgresProjectionSource{DB: db},
			Heartbeats: &recovery.TemporalHeartbeatSource{Client: c},
		},
		Controller: &recovery.TemporalController{Client: c, Namespace: envOr("TEMPORAL_NAMESPACE", "default")},
		Notifier:   &neverNotifier{t: t},
		Config:     recovery.Config{StaleAfter: 50 * time.Millisecond, NoProgressAfter: time.Hour, RetryBudget: 1000},
	}

	// The workflow's StartTime is the only heartbeat signal available
	// (no activity was ever scheduled) — sleeping past StaleAfter
	// guarantees Classify sees it as stale, deterministically, without
	// racing Temporal's own clock.
	time.Sleep(200 * time.Millisecond)

	results, err := sup.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	got, found := resultFor(results, workflowID)
	if !found {
		t.Fatalf("ScanOnce returned no result for %s; results=%+v", workflowID, results)
	}
	if got.Condition != recovery.DeadWorker {
		t.Fatalf("Condition = %v, want DeadWorker", got.Condition)
	}
	if got.Action != recovery.ActionRepaired {
		t.Fatalf("Action = %v, want ActionRepaired (err=%v)", got.Action, got.Err)
	}

	desc, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		t.Fatalf("describe workflow after reset: %v", err)
	}
	newRunID := desc.GetWorkflowExecutionInfo().GetExecution().GetRunId()
	if newRunID == originalRunID {
		t.Fatalf("RunID unchanged after repair (%s) — Reset did not actually reset the execution", newRunID)
	}
}

// TestRecoverySupervisorLive_MissingWakeIsEscalated manufactures a
// WAITING workflow_status_projection row with no wake_at and an
// unrecognized wait reason, and asserts one real Supervisor.ScanOnce
// classifies it MissingWake and escalates it through a real
// *notify.Engine backed by a real Postgres notifications table (Task
// 30) — proven by a real row landing in that table, not merely a nil
// error from a fake Notifier.
func TestRecoverySupervisorLive_MissingWakeIsEscalated(t *testing.T) {
	pgDSN, _ := liveEnv(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ensureLiveTables(t, db)

	workflowID := fmt.Sprintf("recovery-live-missingwake-%d", time.Now().UnixNano())
	insertProjectionRow(t, db, workflowID, state.StatusWaiting, state.Reason("unrecognized-wait-reason"), nil)

	engine := notify.NewEngine(
		notify.NewPostgresStore(db),
		&notify.HTTPSender{Token: "unused-in-this-test"}, // Ingest() only enqueues; it never sends.
		notify.NewRateLimiter(notify.DefaultLimits()),
		notify.Config{},
	)

	sup := &recovery.Supervisor{
		Source:     &recovery.PostgresProjectionSource{DB: db}, // no RUNNING rows here — no Temporal client needed
		Controller: refusingController{},
		Notifier:   engine,
		OpsChatID:  "recovery-live-test-chat",
	}

	results, err := sup.ScanOnce(ctx)
	if err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	got, found := resultFor(results, workflowID)
	if !found {
		t.Fatalf("ScanOnce returned no result for %s; results=%+v", workflowID, results)
	}
	if got.Condition != recovery.MissingWake {
		t.Fatalf("Condition = %v, want MissingWake", got.Condition)
	}
	if got.Action != recovery.ActionEscalated {
		t.Fatalf("Action = %v, want ActionEscalated (err=%v)", got.Action, got.Err)
	}

	dedupeKey := fmt.Sprintf("liveness:%s:%s", workflowID, recovery.MissingWake)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM notifications WHERE id = $1`, dedupeKey) })

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE id = $1`, dedupeKey).Scan(&count); err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	if count != 1 {
		t.Fatalf("notifications row count for %s = %d, want 1 — escalation did not really persist", dedupeKey, count)
	}
}

// refusingController is a recovery.WorkflowController that fails every
// Reset call — used for the MissingWake test, where a repair is never
// expected to be attempted (MissingWake is never in repairableConditions),
// so any call at all is a test bug, not a live-infra gap.
type refusingController struct{}

func (refusingController) Reset(context.Context, string) error {
	return fmt.Errorf("recovery live test: Reset should never be called for MissingWake")
}

// neverNotifier is a recovery.Notifier that fails every Ingest call —
// used for the DeadWorker test, where escalation is never expected
// (DeadWorker is repairable, so a successful Reset never escalates), so
// any call at all is a test bug, not a live-infra gap.
type neverNotifier struct{ t *testing.T }

func (n *neverNotifier) Ingest(context.Context, notify.Event) error {
	n.t.Helper()
	n.t.Fatal("recovery live test: Ingest should never be called for a successfully repaired DeadWorker")
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
