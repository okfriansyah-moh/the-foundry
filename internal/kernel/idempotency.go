package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// IdempotencyKey identifies one logical attempt at one activity within one
// workflow/task, per docs/PLAN.md Task 12 Step 5: (wfID, taskID, activity,
// attempt-scope). Attempt is a workflow-assigned logical attempt counter
// (how many times the workflow has deliberately retried this task), not
// Temporal's own per-call activity.Attempt — a Temporal-level retry of the
// same logical attempt must coalesce onto the same receipt.
type IdempotencyKey struct {
	WorkflowID string
	TaskID     string
	Activity   string
	Attempt    int
}

// String renders k as the flat string ReceiptStore keys are stored under.
func (k IdempotencyKey) String() string {
	return fmt.Sprintf("%s|%s|%s|%d", k.WorkflowID, k.TaskID, k.Activity, k.Attempt)
}

// ReceiptStore records the outcome of a completed activity invocation
// keyed by IdempotencyKey, so re-executing an already-completed activity
// (e.g. after a Temporal-level retry of a call whose side effect actually
// landed) returns the recorded receipt instead of repeating the side
// effect.
type ReceiptStore interface {
	Get(ctx context.Context, key string) (payload []byte, found bool, err error)
	Put(ctx context.Context, key string, payload []byte) error
}

// withReceipt runs fn at most once per key: if a receipt already exists it
// is decoded and returned directly; otherwise fn runs, and only a
// successful result is recorded — a failing fn is left unrecorded so
// Temporal's activity retry policy can legitimately retry it.
func withReceipt[T any](ctx context.Context, store ReceiptStore, key string, fn func() (T, error)) (T, error) {
	var zero T

	if raw, found, err := store.Get(ctx, key); err != nil {
		return zero, fmt.Errorf("kernel: read receipt %s: %w", key, err)
	} else if found {
		var out T
		if err := json.Unmarshal(raw, &out); err != nil {
			return zero, fmt.Errorf("kernel: decode receipt %s: %w", key, err)
		}
		return out, nil
	}

	result, err := fn()
	if err != nil {
		return zero, err
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return zero, fmt.Errorf("kernel: encode receipt %s: %w", key, err)
	}
	if err := store.Put(ctx, key, payload); err != nil {
		return zero, fmt.Errorf("kernel: write receipt %s: %w", key, err)
	}
	return result, nil
}

// MemReceiptStore is an in-memory ReceiptStore for tests and any run
// without a live Postgres.
type MemReceiptStore struct {
	mu   sync.Mutex
	rows map[string][]byte
}

// NewMemReceiptStore returns an empty MemReceiptStore.
func NewMemReceiptStore() *MemReceiptStore {
	return &MemReceiptStore{rows: make(map[string][]byte)}
}

// Get implements ReceiptStore.
func (s *MemReceiptStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.rows[key]
	return raw, ok, nil
}

// Put implements ReceiptStore.
func (s *MemReceiptStore) Put(_ context.Context, key string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[key] = payload
	return nil
}

// PGReceiptStore is the Postgres-backed ReceiptStore (internal/db/migrations/00002).
type PGReceiptStore struct {
	db *sql.DB
}

// NewPGReceiptStore wraps an existing *sql.DB.
func NewPGReceiptStore(db *sql.DB) *PGReceiptStore { return &PGReceiptStore{db: db} }

// Get implements ReceiptStore.
func (s *PGReceiptStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	const q = `SELECT payload FROM receipts WHERE key = $1`
	var payload []byte
	err := s.db.QueryRowContext(ctx, q, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("kernel: get receipt %s: %w", key, err)
	}
	return payload, true, nil
}

// Put implements ReceiptStore. Receipts are immutable once written — a
// duplicate Put for the same key is a no-op, not an overwrite, matching
// evidence bundle semantics.
func (s *PGReceiptStore) Put(ctx context.Context, key string, payload []byte) error {
	const q = `
INSERT INTO receipts (key, payload, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, q, key, payload, time.Now().UTC()); err != nil {
		return fmt.Errorf("kernel: put receipt %s: %w", key, err)
	}
	return nil
}
