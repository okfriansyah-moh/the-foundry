package evolve

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// FreezeScopeGlobal is the scope of the daemon-wide promotion freeze latch.
const FreezeScopeGlobal = "global"

const freezeAdvisoryNamespace = "foundry:evolve:promotion-freeze:v1\x00"

// FreezeStore is the durable, Postgres-backed promotion-freeze latch
// (docs/PLAN.md Task 127). It replaces the process-global atomic.Bool for any
// caller that needs the freeze to be visible across processes (the daemon and
// the CLI) and to survive a restart. The in-process Freeze/Unfreeze helpers in
// budget.go remain for the hot-path latch a single worker reads, but the
// authoritative, durable state lives here.
type FreezeStore struct {
	db *sql.DB
}

// NewFreezeStore wraps an existing *sql.DB.
func NewFreezeStore(db *sql.DB) *FreezeStore { return &FreezeStore{db: db} }

// Freeze durably records a freeze for scope with the given reason, idempotently
// (a repeated freeze refreshes the reason and timestamp). Freeze and promotion
// activation serialize on the same transaction-scoped advisory lock.
func (s *FreezeStore) Freeze(ctx context.Context, scope string, reason FreezeCondition) error {
	scope = normalizeFreezeScope(scope)
	tx, err := s.begin(ctx, "freeze", scope)
	if err != nil {
		return err
	}
	if err := lockFreezeScope(ctx, tx, scope); err != nil {
		return rollbackFreezeTx(tx, err, "freeze", scope)
	}
	const q = `
INSERT INTO improvement_freeze (scope, reason, frozen_at)
VALUES ($1, $2, now())
ON CONFLICT (scope) DO UPDATE SET reason = EXCLUDED.reason, frozen_at = now()`
	if _, err := tx.ExecContext(ctx, q, scope, string(reason)); err != nil {
		return rollbackFreezeTx(tx, fmt.Errorf("evolve: freeze %q: %w", scope, err), "freeze", scope)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("evolve: commit freeze %q: %w", scope, err)
	}
	return nil
}

// Unfreeze durably clears the freeze for scope and reports whether a freeze
// existed (so a caller can audit "cleared an active freeze" vs "no-op").
// Unfreeze takes the activation lock too, preventing a thaw from changing
// beneath an activation decision.
func (s *FreezeStore) Unfreeze(ctx context.Context, scope string) (bool, error) {
	scope = normalizeFreezeScope(scope)
	tx, err := s.begin(ctx, "unfreeze", scope)
	if err != nil {
		return false, err
	}
	if err := lockFreezeScope(ctx, tx, scope); err != nil {
		return false, rollbackFreezeTx(tx, err, "unfreeze", scope)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM improvement_freeze WHERE scope = $1`, scope)
	if err != nil {
		return false, rollbackFreezeTx(tx, fmt.Errorf("evolve: unfreeze %q: %w", scope, err), "unfreeze", scope)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, rollbackFreezeTx(tx, fmt.Errorf("evolve: unfreeze %q: %w", scope, err), "unfreeze", scope)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("evolve: commit unfreeze %q: %w", scope, err)
	}
	return n > 0, nil
}

// PromotionGuard keeps the durable freeze decision stable across activation.
// Commit or Rollback releases the transaction-scoped advisory lock. Its zero
// value is an idempotent no-op guard for deterministic in-memory test gates.
type PromotionGuard struct {
	mu   sync.Mutex
	tx   *sql.Tx
	done bool
}

// Commit releases the promotion lock after activation. Because canonical skill
// files are not part of this PostgreSQL transaction, a commit error means lock
// release is uncertain; callers must report an activation-recovery error rather
// than assume that the already-written filesystem state was rolled back.
func (g *PromotionGuard) Commit() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done || g.tx == nil {
		g.done = true
		return nil
	}
	g.done = true
	if err := g.tx.Commit(); err != nil {
		return fmt.Errorf("evolve: commit promotion guard: %w", err)
	}
	return nil
}

// Rollback abandons activation and releases the promotion lock. It is safe to
// call more than once and safe on a zero-value guard.
func (g *PromotionGuard) Rollback() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done || g.tx == nil {
		g.done = true
		return nil
	}
	g.done = true
	if err := g.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("evolve: rollback promotion guard: %w", err)
	}
	return nil
}

// AcquirePromotionGuard serializes activation against Freeze and Unfreeze for
// scope, then checks improvement_freeze within that same transaction. A frozen
// result has no guard because its transaction has already been rolled back.
func (s *FreezeStore) AcquirePromotionGuard(ctx context.Context, scope string) (*PromotionGuard, bool, FreezeCondition, error) {
	scope = normalizeFreezeScope(scope)
	// The acquisition queries still use ctx, so cancellation interrupts waiting
	// for the advisory lock. The transaction itself deliberately outlives ctx:
	// after acquisition, a cancellation must not silently release the lock while
	// the bridge is partway through filesystem activation. The bridge observes
	// ctx and explicitly rolls the guard back on its error path.
	tx, err := s.begin(context.WithoutCancel(ctx), "promotion guard", scope)
	if err != nil {
		return nil, true, "", err
	}
	rollback := func(cause error) (*PromotionGuard, bool, FreezeCondition, error) {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return nil, true, "", errors.Join(cause, fmt.Errorf("evolve: rollback promotion guard %q: %w", scope, rollbackErr))
		}
		return nil, true, "", cause
	}
	if err := lockFreezeScope(ctx, tx, scope); err != nil {
		return rollback(err)
	}
	var reason string
	err = tx.QueryRowContext(ctx, `SELECT reason FROM improvement_freeze WHERE scope = $1`, scope).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return &PromotionGuard{tx: tx}, false, "", nil
	}
	if err != nil {
		return rollback(fmt.Errorf("evolve: guarded is-frozen %q: %w", scope, err))
	}
	if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		return nil, true, FreezeCondition(reason), fmt.Errorf("evolve: release frozen promotion guard %q: %w", scope, rollbackErr)
	}
	return nil, true, FreezeCondition(reason), nil
}

func (s *FreezeStore) begin(ctx context.Context, operation, scope string) (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evolve: begin %s %q: freeze store database is nil", operation, scope)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("evolve: begin %s %q: %w", operation, scope, err)
	}
	return tx, nil
}

func rollbackFreezeTx(tx *sql.Tx, cause error, operation, scope string) error {
	if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		return errors.Join(cause, fmt.Errorf("evolve: rollback %s %q: %w", operation, scope, rollbackErr))
	}
	return cause
}

func lockFreezeScope(ctx context.Context, tx *sql.Tx, scope string) error {
	key1, key2 := freezeAdvisoryKey(scope)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, key1, key2); err != nil {
		return fmt.Errorf("evolve: lock freeze scope %q: %w", scope, err)
	}
	return nil
}

// freezeAdvisoryKey uses 64 bits from a namespace-separated SHA-256 digest and
// PostgreSQL's two-int advisory-lock form. The namespace prevents accidental
// overlap with unrelated advisory-lock domains; using both halves keeps the
// collision space at 64 bits rather than truncating it to one int32.
func freezeAdvisoryKey(scope string) (int32, int32) {
	digest := sha256.Sum256([]byte(freezeAdvisoryNamespace + scope))
	return int32(binary.BigEndian.Uint32(digest[0:4])), int32(binary.BigEndian.Uint32(digest[4:8]))
}

func normalizeFreezeScope(scope string) string {
	if scope == "" {
		return FreezeScopeGlobal
	}
	return scope
}

// IsFrozen reports whether scope is currently frozen and, if so, its reason.
// Activation code must use AcquirePromotionGuard rather than relying on this
// read-only snapshot across side effects.
func (s *FreezeStore) IsFrozen(ctx context.Context, scope string) (bool, FreezeCondition, error) {
	scope = normalizeFreezeScope(scope)
	if s == nil || s.db == nil {
		return true, "", fmt.Errorf("evolve: is-frozen %q: freeze store database is nil", scope)
	}
	var reason string
	err := s.db.QueryRowContext(ctx, `SELECT reason FROM improvement_freeze WHERE scope = $1`, scope).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return true, "", fmt.Errorf("evolve: is-frozen %q: %w", scope, err)
	}
	return true, FreezeCondition(reason), nil
}
