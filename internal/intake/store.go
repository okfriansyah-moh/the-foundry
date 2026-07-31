package intake

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ErrRunNotFound is returned when a run lookup by ID finds no row.
var ErrRunNotFound = errors.New("intake: run not found")

// Store persists intake runs and their per-stage records. Two backends
// implement it: MemStore (tests, zero-dependency) and PGStore (production,
// Postgres). Stage records are append-only and unique per (run, stage), which
// is what makes stage re-execution idempotent.
type Store interface {
	// CreateRun persists a new run and returns it as stored. The caller
	// (Pipeline.Start) sets CreatedAt/UpdatedAt from its clock before calling;
	// the store persists those values verbatim rather than assigning its own.
	CreateRun(ctx context.Context, r Run) (Run, error)
	// GetRun returns the run, or ErrRunNotFound.
	GetRun(ctx context.Context, runID string) (Run, error)
	// ListRuns returns the most recent runs, newest first (limit<=0 → 50).
	ListRuns(ctx context.Context, limit int) ([]Run, error)
	// UpdateRun persists the run's mutable fields (stage, status, spend,
	// mission id, updated_at).
	UpdateRun(ctx context.Context, r Run) error
	// RecordStage appends a completed stage record. It must be a no-op error
	// on a duplicate (run, stage) so replay is safe.
	RecordStage(ctx context.Context, rec StageRecord) error
	// GetStage returns a previously recorded stage output, and ok=false when
	// the stage has not been recorded yet.
	GetStage(ctx context.Context, runID string, stage Stage) (StageRecord, bool, error)
}

// MemStore is an in-memory Store for tests. It is safe for concurrent use.
type MemStore struct {
	mu     sync.Mutex
	runs   map[string]Run
	stages map[string]map[Stage]StageRecord
	seq    int64
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{runs: map[string]Run{}, stages: map[string]map[Stage]StageRecord{}}
}

// CreateRun implements Store.
func (m *MemStore) CreateRun(_ context.Context, r Run) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if _, exists := m.runs[r.ID]; exists {
		return Run{}, errors.New("intake: run already exists")
	}
	m.runs[r.ID] = r
	m.stages[r.ID] = map[Stage]StageRecord{}
	return r, nil
}

// GetRun implements Store.
func (m *MemStore) GetRun(_ context.Context, runID string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return r, nil
}

// ListRuns implements Store.
func (m *MemStore) ListRuns(_ context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Run, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// UpdateRun implements Store.
func (m *MemStore) UpdateRun(_ context.Context, r Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.ID]; !ok {
		return ErrRunNotFound
	}
	m.runs[r.ID] = r
	return nil
}

// RecordStage implements Store. Duplicate (run, stage) is a silent no-op so a
// replay never double-records.
func (m *MemStore) RecordStage(_ context.Context, rec StageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	byStage, ok := m.stages[rec.RunID]
	if !ok {
		return ErrRunNotFound
	}
	if _, exists := byStage[rec.Stage]; exists {
		return nil
	}
	byStage[rec.Stage] = rec
	return nil
}

// GetStage implements Store.
func (m *MemStore) GetStage(_ context.Context, runID string, stage Stage) (StageRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byStage, ok := m.stages[runID]
	if !ok {
		return StageRecord{}, false, ErrRunNotFound
	}
	rec, ok := byStage[stage]
	return rec, ok, nil
}
