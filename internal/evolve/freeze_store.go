package evolve

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// FreezeScopeGlobal is the scope of the daemon-wide promotion freeze latch.
const FreezeScopeGlobal = "global"

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
// (a repeated freeze refreshes the reason and timestamp).
func (s *FreezeStore) Freeze(ctx context.Context, scope string, reason FreezeCondition) error {
	if scope == "" {
		scope = FreezeScopeGlobal
	}
	const q = `
INSERT INTO improvement_freeze (scope, reason, frozen_at)
VALUES ($1, $2, now())
ON CONFLICT (scope) DO UPDATE SET reason = EXCLUDED.reason, frozen_at = now()`
	if _, err := s.db.ExecContext(ctx, q, scope, string(reason)); err != nil {
		return fmt.Errorf("evolve: freeze %q: %w", scope, err)
	}
	return nil
}

// Unfreeze durably clears the freeze for scope and reports whether a freeze
// existed (so a caller can audit "cleared an active freeze" vs "no-op").
func (s *FreezeStore) Unfreeze(ctx context.Context, scope string) (bool, error) {
	if scope == "" {
		scope = FreezeScopeGlobal
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM improvement_freeze WHERE scope = $1`, scope)
	if err != nil {
		return false, fmt.Errorf("evolve: unfreeze %q: %w", scope, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("evolve: unfreeze %q: %w", scope, err)
	}
	return n > 0, nil
}

// IsFrozen reports whether scope is currently frozen and, if so, its reason.
func (s *FreezeStore) IsFrozen(ctx context.Context, scope string) (bool, FreezeCondition, error) {
	if scope == "" {
		scope = FreezeScopeGlobal
	}
	var reason string
	err := s.db.QueryRowContext(ctx, `SELECT reason FROM improvement_freeze WHERE scope = $1`, scope).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("evolve: is-frozen %q: %w", scope, err)
	}
	return true, FreezeCondition(reason), nil
}
