package extops_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// testDSN returns the DSN to run real-Postgres extops tests against, or
// "" if none is configured. EXTOPS_TEST_PG_DSN is checked first (matches
// internal/projection's PROJECTION_TEST_PG_DSN precedent); PG_DSN is
// checked second so these tests run for free inside the dev container
// (deploy/docker-compose.yaml sets PG_DSN for the dev service without any
// extra wiring).
func testDSN() string {
	if v := os.Getenv("EXTOPS_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured, otherwise
// returns a live connection with external_operations guaranteed to exist
// (internal/db/migrations/00006_ledgers.sql's shape, created here too so
// this test does not depend on `make migrate-up` having already run).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("EXTOPS_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/ledger/...` for a real Postgres")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	const ddl = `
CREATE TABLE IF NOT EXISTS external_operations (
    id              TEXT PRIMARY KEY,
    workflow_id     TEXT NOT NULL,
    kind            TEXT NOT NULL,
    target          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    state           TEXT NOT NULL CHECK (state IN ('reserved', 'executed', 'reconciled', 'failed')),
    request         JSONB NOT NULL,
    receipt         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("ensure external_operations exists: %v", err)
	}

	return db
}

// uniqueKey returns a random idempotency key so repeated test runs (e.g.
// `-count=10`) never collide against rows a prior run left behind.
func uniqueKey(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate unique key: %v", err)
	}
	return t.Name() + "-" + hex.EncodeToString(buf)
}

func TestStore_Reserve_DuplicateKeyIsNoOp(t *testing.T) {
	db := openTestDB(t)
	store := extops.NewStore(db)
	ctx := context.Background()
	key := uniqueKey(t)

	first, err := store.Reserve(ctx, "wf-1", "scm.push", "org/repo#main", key, map[string]string{"sha": "abc"})
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if first.State != extops.StateReserved {
		t.Fatalf("state = %q, want reserved", first.State)
	}

	second, err := store.Reserve(ctx, "wf-1", "scm.push", "org/repo#main", key, map[string]string{"sha": "abc"})
	if err != nil {
		t.Fatalf("second reserve (duplicate key): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second reserve returned a different OpID (%s != %s) — duplicate row created", second.ID, first.ID)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM external_operations WHERE idempotency_key = $1`, key).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for key = %d, want 1", count)
	}
}

func TestStore_MarkExecuted_RejectsNeverReserved(t *testing.T) {
	db := openTestDB(t)
	store := extops.NewStore(db)
	ctx := context.Background()

	_, err := store.MarkExecuted(ctx, extops.OpID("does-not-exist"), map[string]string{"ok": "true"})
	if err == nil {
		t.Fatal("MarkExecuted on an unknown OpID must error, got nil")
	}
}

func TestStore_MarkExecuted_RejectsAlreadyExecuted(t *testing.T) {
	db := openTestDB(t)
	store := extops.NewStore(db)
	ctx := context.Background()
	key := uniqueKey(t)

	op, err := store.Reserve(ctx, "wf-1", "scm.push", "org/repo#main", key, map[string]string{"sha": "abc"})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := store.MarkExecuted(ctx, op.ID, map[string]string{"result": "ok"}); err != nil {
		t.Fatalf("first mark executed: %v", err)
	}

	if _, err := store.MarkExecuted(ctx, op.ID, map[string]string{"result": "ok-again"}); err == nil {
		t.Fatal("second MarkExecuted on an already-executed op must error, got nil")
	}
}

func TestStore_Reconcile_OnlyAppliesToExecuted(t *testing.T) {
	db := openTestDB(t)
	store := extops.NewStore(db)
	ctx := context.Background()
	key := uniqueKey(t)

	op, err := store.Reserve(ctx, "wf-1", "scm.push", "org/repo#main", key, map[string]string{"sha": "abc"})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	if _, err := store.Reconcile(ctx, op.ID, []byte(`{"sha":"abc"}`)); err == nil {
		t.Fatal("Reconcile on a merely-reserved op must error, got nil")
	}

	if _, err := store.MarkExecuted(ctx, op.ID, map[string]string{"sha": "abc"}); err != nil {
		t.Fatalf("mark executed: %v", err)
	}

	diverged, err := store.Reconcile(ctx, op.ID, []byte(`{"sha":"abc"}`))
	if err != nil {
		t.Fatalf("reconcile matching observation: %v", err)
	}
	if diverged {
		t.Fatal("reconcile reported divergence for an observation matching the receipt")
	}

	reReconciled, err := store.Get(ctx, op.ID)
	if err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	if reReconciled.State != extops.StateReconciled {
		t.Fatalf("state after reconcile = %q, want reconciled", reReconciled.State)
	}
}

func TestStore_Reconcile_DetectsDivergence(t *testing.T) {
	db := openTestDB(t)
	store := extops.NewStore(db)
	ctx := context.Background()
	key := uniqueKey(t)

	op, err := store.Reserve(ctx, "wf-1", "scm.push", "org/repo#main", key, map[string]string{"sha": "abc"})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := store.MarkExecuted(ctx, op.ID, map[string]string{"sha": "abc"}); err != nil {
		t.Fatalf("mark executed: %v", err)
	}

	diverged, err := store.Reconcile(ctx, op.ID, []byte(`{"sha":"different"}`))
	if err != nil {
		t.Fatalf("reconcile diverging observation: %v", err)
	}
	if !diverged {
		t.Fatal("reconcile did not report divergence for an observation that disagrees with the receipt")
	}
}
