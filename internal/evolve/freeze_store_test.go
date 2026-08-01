package evolve_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
)

func openFreezeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set — skipping; run via `make test` or docker for a real Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const ddl = `
CREATE TABLE IF NOT EXISTS improvement_freeze (
    scope     TEXT PRIMARY KEY,
    reason    TEXT NOT NULL,
    frozen_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("create improvement_freeze: %v", err)
	}
	return db
}

// TestFreezeStore_DurableAcrossReopen proves docs/PLAN.md Task 127: a freeze is
// durable — a fresh FreezeStore over a NEW connection (the in-process analogue
// of a daemon restart) still observes it, and unfreeze clears it durably.
func TestFreezeStore_DurableAcrossReopen(t *testing.T) {
	db := openFreezeTestDB(t)
	ctx := context.Background()
	scope := "product-" + t.Name()
	_, _ = db.ExecContext(ctx, `DELETE FROM improvement_freeze WHERE scope = $1`, scope)

	writer := evolve.NewFreezeStore(db)
	if err := writer.Freeze(ctx, scope, evolve.FreezeBudgetExceeded); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	// A fresh store (models a different process / a restart) sees the freeze.
	reader := evolve.NewFreezeStore(db)
	frozen, reason, err := reader.IsFrozen(ctx, scope)
	if err != nil {
		t.Fatalf("is-frozen: %v", err)
	}
	if !frozen || reason != evolve.FreezeBudgetExceeded {
		t.Fatalf("expected durable freeze budget-exceeded, got frozen=%v reason=%q", frozen, reason)
	}

	// Unfreeze clears it durably and reports it cleared an active freeze.
	cleared, err := reader.Unfreeze(ctx, scope)
	if err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	if !cleared {
		t.Fatal("unfreeze must report it cleared an active freeze")
	}
	frozen, _, err = writer.IsFrozen(ctx, scope)
	if err != nil {
		t.Fatalf("is-frozen after unfreeze: %v", err)
	}
	if frozen {
		t.Fatal("freeze must be gone after unfreeze")
	}
	// A second unfreeze is a no-op that reports nothing was cleared.
	if cleared, _ := reader.Unfreeze(ctx, scope); cleared {
		t.Fatal("second unfreeze must report no active freeze")
	}
}
