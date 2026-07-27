package recovery_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// testDSN mirrors internal/notify/internal/ledger's testDSN precedent:
// RECOVERY_TEST_PG_DSN first, PG_DSN second (set for free inside the dev
// container by deploy/docker-compose.yaml).
func testDSN() string {
	if v := os.Getenv("RECOVERY_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured — this is
// why `go test ./internal/recovery/... -race` passes with no live infra;
// `make test` (Docker, PG_DSN set) exercises this for real.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("RECOVERY_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/recovery/...` for a real Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Mirrors internal/db/migrations/00003_projection.sql's schema —
	// inlined here (rather than run via the real migration tool) so this
	// test is isolated from whatever else that table may hold in a
	// shared dev database, matching internal/notify/store_test.go's own
	// precedent.
	const createTable = `
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
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("create workflow_status_projection: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM workflow_status_projection`); err != nil {
		t.Fatalf("truncate workflow_status_projection: %v", err)
	}
	return db
}

func TestPostgresProjectionSource_ListNonterminal(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	wakeAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	// Workflow IDs are unique per test run and every inserted row is
	// deleted in t.Cleanup — this table is a shared dev database other
	// concurrent tests/sessions may be reading via the exact same
	// ListNonterminal query, so a fixed "wf-running"-style ID that
	// outlives this test (no cleanup) previously leaked into and broke
	// an unrelated live-Temporal test that scanned every nonterminal row
	// in the table, not just its own.
	prefix := fmt.Sprintf("recovery-pgtest-%d-", time.Now().UnixNano())
	rows := []struct {
		id     string
		status state.Status
		reason state.Reason
		wakeAt *time.Time
	}{
		{id: prefix + "running", status: state.StatusRunning, reason: "", wakeAt: nil},
		{id: prefix + "waiting", status: state.StatusWaiting, reason: state.ReasonHumanApproval, wakeAt: &wakeAt},
		{id: prefix + "succeeded", status: state.StatusSucceeded, reason: "", wakeAt: nil},
		{id: prefix + "failed", status: state.StatusFailed, reason: "", wakeAt: nil},
		{id: prefix + "cancelled", status: state.StatusCancelled, reason: "", wakeAt: nil},
	}
	for _, r := range rows {
		_, err := db.ExecContext(ctx, `
INSERT INTO workflow_status_projection (workflow_id, status, reason, attempt, wake_at, last_seq, projector_version)
VALUES ($1, $2, $3, $4, $5, 1, 'v1')`,
			r.id, string(r.status), string(r.reason), 2, r.wakeAt)
		if err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
		t.Cleanup(func(id string) func() {
			return func() { _, _ = db.Exec(`DELETE FROM workflow_status_projection WHERE workflow_id = $1`, id) }
		}(r.id))
	}

	src := &recovery.PostgresProjectionSource{DB: db}
	snaps, err := src.ListNonterminal(ctx)
	if err != nil {
		t.Fatalf("ListNonterminal: %v", err)
	}

	byID := make(map[string]recovery.WorkflowSnapshot)
	for _, s := range snaps {
		byID[s.WorkflowID] = s
	}
	for _, terminalID := range []string{prefix + "succeeded", prefix + "failed", prefix + "cancelled"} {
		if _, present := byID[terminalID]; present {
			t.Fatalf("terminal row %s present in ListNonterminal result, want excluded", terminalID)
		}
	}
	running, ok := byID[prefix+"running"]
	if !ok {
		t.Fatal("running row missing from ListNonterminal result")
	}
	if running.Status != state.StatusRunning || running.Attempt != 2 || running.WakeAt != nil {
		t.Fatalf("running snapshot = %+v, want status=RUNNING attempt=2 wakeAt=nil", running)
	}
	if running.LastProgressAt.IsZero() {
		t.Fatal("running row's LastProgressAt should be populated from updated_at")
	}

	waiting, ok := byID[prefix+"waiting"]
	if !ok {
		t.Fatal("waiting row missing from ListNonterminal result")
	}
	if waiting.Reason != state.ReasonHumanApproval {
		t.Fatalf("waiting Reason = %q, want %q", waiting.Reason, state.ReasonHumanApproval)
	}
	if waiting.WakeAt == nil || !waiting.WakeAt.Equal(wakeAt) {
		t.Fatalf("waiting WakeAt = %v, want %v", waiting.WakeAt, wakeAt)
	}
}
