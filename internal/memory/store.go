package memory

import (
	"context"
	"sort"
	"sync"
)

// Store persists curated memories. Retrieval is deliberately profile-scoped:
// there is no method that returns a memory without naming a profile, so a
// caller cannot read across profiles by construction. Deletion, by contrast,
// is by source (global) — deleting a source evidence ref must remove its
// derived memories everywhere.
type Store interface {
	// Put stores or replaces a memory by its ID.
	Put(ctx context.Context, m Memory) error
	// GetForProfile returns the memory with id, but only if it is scoped to
	// profile; otherwise ok is false (no cross-profile read).
	GetForProfile(ctx context.Context, profile, id string) (Memory, bool, error)
	// ListByProfile returns every memory scoped to profile, in a
	// deterministic order. It never returns another profile's memories.
	ListByProfile(ctx context.Context, profile string) ([]Memory, error)
	// DeleteBySource deletes every memory whose EvidenceRefs contains ref and
	// returns the IDs deleted (for vector-index cascade). This is the Task 66
	// deletion-cascade hook.
	DeleteBySource(ctx context.Context, ref string) ([]string, error)
}

// MemStore is an in-memory Store for tests and any run without Postgres.
type MemStore struct {
	mu   sync.Mutex
	rows map[string]Memory
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{rows: make(map[string]Memory)}
}

// Put implements Store.
func (s *MemStore) Put(_ context.Context, m Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[m.ID] = cloneMemory(m)
	return nil
}

// GetForProfile implements Store — profile-scoped, no cross-profile read.
func (s *MemStore) GetForProfile(_ context.Context, profile, id string) (Memory, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.rows[id]
	if !ok || m.ProfileScope != profile {
		return Memory{}, false, nil
	}
	return cloneMemory(m), true, nil
}

// ListByProfile implements Store.
func (s *MemStore) ListByProfile(_ context.Context, profile string) ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Memory
	for _, m := range s.rows {
		if m.ProfileScope == profile {
			out = append(out, cloneMemory(m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// DeleteBySource implements Store.
func (s *MemStore) DeleteBySource(_ context.Context, ref string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var deleted []string
	for id, m := range s.rows {
		if containsRef(m.EvidenceRefs, ref) {
			deleted = append(deleted, id)
			delete(s.rows, id)
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

func cloneMemory(m Memory) Memory {
	refs := make([]string, len(m.EvidenceRefs))
	copy(refs, m.EvidenceRefs)
	m.EvidenceRefs = refs
	return m
}

func containsRef(refs []string, ref string) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
