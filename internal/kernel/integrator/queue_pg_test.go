package integrator_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
)

func testDSN() string {
	if v := os.Getenv("INTEGRATOR_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

func openQueueDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("set PG_DSN or INTEGRATOR_TEST_PG_DSN to run the PG integration-queue test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS integration_queue (
			id TEXT PRIMARY KEY, branch TEXT NOT NULL, group_id TEXT NOT NULL,
			manifest_digest TEXT NOT NULL, commits TEXT[] NOT NULL DEFAULT '{}',
			expected_base TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
			enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(), processed_at TIMESTAMPTZ, error_msg TEXT)`,
		`CREATE TABLE IF NOT EXISTS integration_receipts (
			id TEXT PRIMARY KEY, branch TEXT NOT NULL, before_sha TEXT NOT NULL,
			after_sha TEXT NOT NULL, group_id TEXT NOT NULL, manifest_digest TEXT NOT NULL,
			issued_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func TestPGQueueEnqueueClaimComplete(t *testing.T) {
	db := openQueueDB(t)
	ctx := context.Background()
	q := integrator.NewPGQueue(db)

	item := integrator.IntegrationItem{
		ID: "it-" + randSuffix(), Branch: "branch-" + randSuffix(), GroupID: "g1",
		ManifestDigest: "d1", Commits: []string{"c1", "c2"}, ExpectedBase: "base-sha",
	}
	if err := q.Enqueue(ctx, item); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := q.Claim(ctx, item.Branch)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if claimed.ID != item.ID || len(claimed.Commits) != 2 {
		t.Fatalf("claimed item mismatch: %+v", claimed)
	}
	// A second claim finds nothing (item is processing).
	if _, ok, err := q.Claim(ctx, item.Branch); err != nil || ok {
		t.Fatalf("second claim must be empty: ok=%v err=%v", ok, err)
	}
	if err := q.Complete(ctx, claimed.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestPGQueueReceiptIdempotency(t *testing.T) {
	db := openQueueDB(t)
	ctx := context.Background()
	q := integrator.NewPGQueue(db)

	r := integrator.Receipt{Branch: "branch-" + randSuffix(), BeforeSHA: "b", AfterSHA: "a", GroupID: "grp-" + randSuffix(), ManifestDigest: "d"}
	if err := q.RecordReceipt(ctx, r); err != nil {
		t.Fatalf("record receipt: %v", err)
	}
	// Recording the same receipt again is a no-op (ON CONFLICT DO NOTHING).
	if err := q.RecordReceipt(ctx, r); err != nil {
		t.Fatalf("record receipt idempotent: %v", err)
	}
	got, ok, err := q.ReceiptForGroup(ctx, r.GroupID, r.Branch)
	if err != nil || !ok {
		t.Fatalf("receipt lookup: ok=%v err=%v", ok, err)
	}
	if got.AfterSHA != "a" {
		t.Fatalf("receipt mismatch: %+v", got)
	}
}

func randSuffix() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
