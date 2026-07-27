package provenance_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// auditTestDSN mirrors internal/observe/deadletter_pg_test.go's own
// precedent: a package-specific env var first, PG_DSN second (set for free
// inside the dev container by deploy/docker-compose.yaml).
func auditTestDSN() string {
	if v := os.Getenv("PROVENANCE_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openAuditTestDB skips the calling test if no DSN is configured, and
// creates audit_log directly from internal/db/migrations/00008_audit.sql's
// own shape (rather than depending on `cmd/foundry migrate up` having been
// run against this test database), then truncates it so each test starts
// from an empty chain regardless of what earlier tasks' own live runs (or
// this task's own m1-exit drill) may have already written to the shared
// dev Postgres.
func openAuditTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := auditTestDSN()
	if dsn == "" {
		t.Skip("PROVENANCE_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/provenance/...` for a real Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const createTable = `
CREATE TABLE IF NOT EXISTS audit_log (
    seq        BIGSERIAL PRIMARY KEY,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    subject    TEXT NOT NULL,
    payload    JSONB NOT NULL,
    prev_hash  BYTEA,
    hash       BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("create audit_log: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE audit_log RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate audit_log: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`TRUNCATE audit_log RESTART IDENTITY`) })
	return db
}

// TestVerifyAuditChain_EmptyChainIsOK covers the zero-rows case explicitly
// (VerifyAuditChain's own doc comment: "an empty table is valid and
// reports OK with RowCount 0").
func TestVerifyAuditChain_EmptyChainIsOK(t *testing.T) {
	db := openAuditTestDB(t)
	ctx := context.Background()

	result, err := provenance.VerifyAuditChain(ctx, db)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if !result.OK || result.RowCount != 0 {
		t.Fatalf("got %+v, want OK=true RowCount=0", result)
	}
}

// TestVerifyAuditChain_UntamperedChainVerifies writes a real chain via
// AppendAuditRow (the same writer `foundry plan revoke` uses) and asserts
// the untouched chain verifies clean — the sanity check every tamper test
// below depends on: without this, a tamper test that "detects" corruption
// could just as easily be a verifier that always reports failure.
func TestVerifyAuditChain_UntamperedChainVerifies(t *testing.T) {
	db := openAuditTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := provenance.AppendAuditRow(ctx, db, "alice", "plan.revoke", "plan-1", []byte(`{"n":1}`)); err != nil {
			t.Fatalf("AppendAuditRow %d: %v", i, err)
		}
	}

	result, err := provenance.VerifyAuditChain(ctx, db)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if !result.OK || result.RowCount != 5 {
		t.Fatalf("got %+v, want OK=true RowCount=5", result)
	}
}

// TestVerifyAuditChain_DetectsPayloadTamper directly UPDATEs one row's
// payload after the fact (bypassing AppendAuditRow entirely, as a real
// attacker or a buggy migration would) and asserts VerifyAuditChain catches
// it — the security-relevant case: does the chain actually detect a
// tampered row, not merely "does the command exit 0".
func TestVerifyAuditChain_DetectsPayloadTamper(t *testing.T) {
	db := openAuditTestDB(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := provenance.AppendAuditRow(ctx, db, "alice", "plan.revoke", "plan-1", []byte(`{"n":1}`)); err != nil {
			t.Fatalf("AppendAuditRow %d: %v", i, err)
		}
	}

	if _, err := db.Exec(`UPDATE audit_log SET payload = $1 WHERE seq = 2`, []byte(`{"n":999}`)); err != nil {
		t.Fatalf("tamper update: %v", err)
	}

	result, err := provenance.VerifyAuditChain(ctx, db)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if result.OK {
		t.Fatal("expected VerifyAuditChain to detect the tampered payload, got OK=true")
	}
	if result.BadSeq != 2 {
		t.Fatalf("BadSeq = %d, want 2", result.BadSeq)
	}
}

// TestVerifyAuditChain_DetectsDeletedRow deletes a middle row outright
// (e.g. an attacker or operator error removing one entry to hide an
// action) and asserts the resulting broken prev_hash link is caught, even
// though every remaining row's own hash is still internally consistent
// with its own stored payload+prev_hash.
func TestVerifyAuditChain_DetectsDeletedRow(t *testing.T) {
	db := openAuditTestDB(t)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if err := provenance.AppendAuditRow(ctx, db, "alice", "plan.revoke", "plan-1", []byte(`{"n":1}`)); err != nil {
			t.Fatalf("AppendAuditRow %d: %v", i, err)
		}
	}

	if _, err := db.Exec(`DELETE FROM audit_log WHERE seq = 2`); err != nil {
		t.Fatalf("delete row: %v", err)
	}

	result, err := provenance.VerifyAuditChain(ctx, db)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if result.OK {
		t.Fatal("expected VerifyAuditChain to detect the deleted row's broken link, got OK=true")
	}
	if result.BrokenLinkSeq != 3 {
		t.Fatalf("BrokenLinkSeq = %d, want 3 (the row immediately after the deleted seq=2)", result.BrokenLinkSeq)
	}
}
