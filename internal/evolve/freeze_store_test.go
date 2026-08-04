package evolve_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

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

func resetFreezeScope(t *testing.T, db *sql.DB, scope string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `DELETE FROM improvement_freeze WHERE scope = $1`, scope); err != nil {
		t.Fatalf("reset freeze scope: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM improvement_freeze WHERE scope = $1`, scope)
	})
}

func waitFreezeResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for freeze operation")
		return nil
	}
}

func waitForAdvisoryWaiter(t *testing.T, db *sql.DB, backendPID int, result <-chan error) {
	t.Helper()
	observeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		select {
		case err := <-result:
			t.Fatalf("freeze returned before waiting on activation lock: %v", err)
		default:
		}
		var waiting bool
		err := db.QueryRowContext(observeCtx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE pid = $1
      AND wait_event_type = 'Lock'
      AND wait_event = 'advisory'
)`, backendPID).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe advisory-lock waiter: %v", err)
		}
		if waiting {
			return
		}
	}
}

// TestFreezeStore_DurableAcrossReopen proves docs/PLAN.md Task 127: a freeze is
// durable — a fresh FreezeStore over a NEW connection (the in-process analogue
// of a daemon restart) still observes it, and unfreeze clears it durably.
func TestFreezeStore_DurableAcrossReopen(t *testing.T) {
	db := openFreezeTestDB(t)
	ctx := context.Background()
	scope := "product-" + t.Name()
	resetFreezeScope(t, db, scope)

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

// TestFreezeStore_FreezeFirstDeniesPromotion proves the freeze decision and
// the promotion guard are serialized across independent store instances. Once
// Freeze commits, a later activation cannot receive a guard.
func TestFreezeStore_FreezeFirstDeniesPromotion(t *testing.T) {
	db := openFreezeTestDB(t)
	ctx := context.Background()
	scope := "product-" + t.Name()
	resetFreezeScope(t, db, scope)

	freezer := evolve.NewFreezeStore(db)
	activator := evolve.NewFreezeStore(db)
	if err := freezer.Freeze(ctx, scope, evolve.FreezeSecurityClassChange); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	guard, frozen, reason, err := activator.AcquirePromotionGuard(ctx, scope)
	if err != nil {
		t.Fatalf("acquire promotion guard: %v", err)
	}
	if guard != nil {
		t.Fatal("frozen scope must not return an activation guard")
	}
	if !frozen || reason != evolve.FreezeSecurityClassChange {
		t.Fatalf("expected security freeze, got frozen=%v reason=%q", frozen, reason)
	}
}

// TestFreezeStore_GuardFirstBlocksFreeze proves the other ordering: when an
// activation owns the guard, a concurrent freeze waits until activation has
// finished instead of changing the decision underneath the catalog write.
func TestFreezeStore_GuardFirstBlocksFreeze(t *testing.T) {
	db := openFreezeTestDB(t)
	freezerDB := openFreezeTestDB(t)
	// Pin the freezer to one backend so pg_stat_activity gives a deterministic
	// handshake that it has reached (and is waiting on) the advisory lock.
	freezerDB.SetMaxOpenConns(1)
	freezerDB.SetMaxIdleConns(1)
	ctx := context.Background()
	scope := "product-" + t.Name()
	resetFreezeScope(t, db, scope)

	activator := evolve.NewFreezeStore(db)
	freezer := evolve.NewFreezeStore(freezerDB)
	guard, frozen, _, err := activator.AcquirePromotionGuard(ctx, scope)
	if err != nil {
		t.Fatalf("acquire promotion guard: %v", err)
	}
	if frozen || guard == nil {
		t.Fatalf("expected live guard for thawed scope, frozen=%v guard=%v", frozen, guard)
	}
	defer func() { _ = guard.Rollback() }()

	var freezerPID int
	if err := freezerDB.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&freezerPID); err != nil {
		t.Fatalf("read freezer backend pid: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		result <- freezer.Freeze(ctx, scope, evolve.FreezeBudgetExceeded)
	}()

	waitForAdvisoryWaiter(t, db, freezerPID, result)
	// The database—not elapsed wall time—now proves the freezer attempted the
	// lock and is blocked. It therefore cannot have mutated the freeze row.
	select {
	case err := <-result:
		t.Fatalf("freeze completed while database reported it blocked: %v", err)
	default:
	}

	if err := guard.Commit(); err != nil {
		t.Fatalf("commit promotion guard: %v", err)
	}
	if err := waitFreezeResult(t, result); err != nil {
		t.Fatalf("freeze after guard release: %v", err)
	}
	frozen, reason, err := activator.IsFrozen(ctx, scope)
	if err != nil {
		t.Fatalf("is-frozen: %v", err)
	}
	if !frozen || reason != evolve.FreezeBudgetExceeded {
		t.Fatalf("expected freeze after release, got frozen=%v reason=%q", frozen, reason)
	}
}

// TestFreezeStore_GuardSurvivesAcquisitionContextCancellation proves caller
// cancellation cannot silently release an acquired guard in the middle of
// filesystem activation. Acquisition remains cancellable; only the lifetime
// of the successfully acquired transaction is detached.
func TestFreezeStore_GuardSurvivesAcquisitionContextCancellation(t *testing.T) {
	db := openFreezeTestDB(t)
	freezerDB := openFreezeTestDB(t)
	freezerDB.SetMaxOpenConns(1)
	freezerDB.SetMaxIdleConns(1)
	scope := "product-" + t.Name()
	resetFreezeScope(t, db, scope)

	acquireCtx, cancelAcquire := context.WithCancel(context.Background())
	guard, frozen, _, err := evolve.NewFreezeStore(db).AcquirePromotionGuard(acquireCtx, scope)
	if err != nil || frozen || guard == nil {
		t.Fatalf("acquire promotion guard: guard=%v frozen=%v err=%v", guard, frozen, err)
	}
	defer func() { _ = guard.Rollback() }()
	cancelAcquire()

	var freezerPID int
	if err := freezerDB.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&freezerPID); err != nil {
		t.Fatalf("read freezer backend pid: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		result <- evolve.NewFreezeStore(freezerDB).Freeze(context.Background(), scope, evolve.FreezeCostSpike)
	}()
	waitForAdvisoryWaiter(t, db, freezerPID, result)

	if err := guard.Rollback(); err != nil {
		t.Fatalf("rollback guard after caller cancellation: %v", err)
	}
	if err := waitFreezeResult(t, result); err != nil {
		t.Fatalf("freeze after explicit release: %v", err)
	}
}

// TestFreezeStore_GuardRollbackReleasesAcrossConnections models an activation
// failure or worker shutdown: rolling its transaction back must release the
// lock so another store/connection can freeze immediately.
func TestFreezeStore_GuardRollbackReleasesAcrossConnections(t *testing.T) {
	db1 := openFreezeTestDB(t)
	db2 := openFreezeTestDB(t)
	ctx := context.Background()
	scope := "product-" + t.Name()
	resetFreezeScope(t, db1, scope)

	guard, frozen, _, err := evolve.NewFreezeStore(db1).AcquirePromotionGuard(ctx, scope)
	if err != nil {
		t.Fatalf("acquire promotion guard: %v", err)
	}
	if frozen || guard == nil {
		t.Fatalf("expected live guard for thawed scope, frozen=%v guard=%v", frozen, guard)
	}
	defer func() { _ = guard.Rollback() }()
	if err := guard.Rollback(); err != nil {
		t.Fatalf("rollback promotion guard: %v", err)
	}
	// Release is deliberately idempotent for defer-based error cleanup.
	if err := guard.Rollback(); err != nil {
		t.Fatalf("second rollback: %v", err)
	}

	freezeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := evolve.NewFreezeStore(db2).Freeze(freezeCtx, scope, evolve.FreezeQualityRegression); err != nil {
		t.Fatalf("cross-connection freeze after rollback: %v", err)
	}
}

// TestFreezeStore_DifferentScopesDoNotContend confirms the advisory key is
// scoped: holding an activation for one product cannot stall another product.
func TestFreezeStore_DifferentScopesDoNotContend(t *testing.T) {
	db1 := openFreezeTestDB(t)
	db2 := openFreezeTestDB(t)
	ctx := context.Background()
	scopeA := "product-a-" + t.Name()
	scopeB := "product-b-" + t.Name()
	resetFreezeScope(t, db1, scopeA)
	resetFreezeScope(t, db1, scopeB)

	guard, frozen, _, err := evolve.NewFreezeStore(db1).AcquirePromotionGuard(ctx, scopeA)
	if err != nil || frozen || guard == nil {
		t.Fatalf("acquire first-scope guard: guard=%v frozen=%v err=%v", guard, frozen, err)
	}
	defer func() { _ = guard.Rollback() }()

	freezeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := evolve.NewFreezeStore(db2).Freeze(freezeCtx, scopeB, evolve.FreezeRollbackChainDepth); err != nil {
		t.Fatalf("unrelated scope must not contend: %v", err)
	}
}

// TestFreezeStore_FailsClosedWithCanceledContext prevents a canceled durable
// check from being mistaken for a thawed result.
func TestFreezeStore_FailsClosedWithCanceledContext(t *testing.T) {
	db := openFreezeTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	guard, frozen, _, err := evolve.NewFreezeStore(db).AcquirePromotionGuard(ctx, "product-"+t.Name())
	if err == nil {
		t.Fatal("canceled guard acquisition must fail")
	}
	if guard != nil || !frozen {
		t.Fatalf("failed guard acquisition must fail closed, guard=%v frozen=%v", guard, frozen)
	}
}
