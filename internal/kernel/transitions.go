package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// TransitionStore appends canonical state.Transition records to the
// durable workflow_transitions stream — the source Task 14's projection
// replays from. Appends are ordered per workflow_id by seq, assigned by
// the store itself.
type TransitionStore interface {
	Append(ctx context.Context, workflowID string, t state.Transition) (seq int64, err error)
}

// MemTransitionStore is an in-memory TransitionStore for tests and any run
// without a live Postgres.
type MemTransitionStore struct {
	mu   sync.Mutex
	rows map[string][]state.Transition
	seq  map[string]int64
}

// NewMemTransitionStore returns an empty MemTransitionStore.
func NewMemTransitionStore() *MemTransitionStore {
	return &MemTransitionStore{
		rows: make(map[string][]state.Transition),
		seq:  make(map[string]int64),
	}
}

// Append implements TransitionStore.
func (s *MemTransitionStore) Append(_ context.Context, workflowID string, t state.Transition) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq[workflowID]++
	seq := s.seq[workflowID]
	s.rows[workflowID] = append(s.rows[workflowID], t)
	return seq, nil
}

// All returns every transition recorded for workflowID, in append order —
// test-only helper.
func (s *MemTransitionStore) All(workflowID string) []state.Transition {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]state.Transition{}, s.rows[workflowID]...)
}

// PGTransitionStore is the Postgres-backed TransitionStore
// (internal/db/migrations/00002_transitions.sql).
type PGTransitionStore struct {
	db *sql.DB
}

// NewPGTransitionStore wraps an existing *sql.DB.
func NewPGTransitionStore(db *sql.DB) *PGTransitionStore { return &PGTransitionStore{db: db} }

// Append implements TransitionStore with a parameterized INSERT; seq is
// assigned by the table's bigserial column.
func (s *PGTransitionStore) Append(ctx context.Context, workflowID string, t state.Transition) (int64, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return 0, fmt.Errorf("kernel: encode transition for %s: %w", workflowID, err)
	}
	const q = `
INSERT INTO workflow_transitions (workflow_id, payload)
VALUES ($1, $2)
RETURNING seq`
	var seq int64
	if err := s.db.QueryRowContext(ctx, q, workflowID, payload).Scan(&seq); err != nil {
		return 0, fmt.Errorf("kernel: append transition for %s: %w", workflowID, err)
	}
	return seq, nil
}
