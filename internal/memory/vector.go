package memory

import (
	"context"
	"sync"
)

// VectorIndex is the optional similarity index over memory content. It lives
// behind this interface so a pgvector-backed implementation can replace the
// in-memory one with no curator change. The one hard requirement on any
// implementation is delete-with-source: when a memory is deleted (because its
// source evidence was deleted), its vector MUST be deleted too.
type VectorIndex interface {
	// Upsert stores or replaces the vector for memoryID.
	Upsert(ctx context.Context, memoryID string, vector []float32) error
	// Delete removes the vector for memoryID. Deleting an absent ID is a
	// no-op, not an error (so cascade deletion is idempotent).
	Delete(ctx context.Context, memoryID string) error
	// Has reports whether a vector exists for memoryID.
	Has(ctx context.Context, memoryID string) (bool, error)
}

// Embedder turns memory content into a vector. Optional — a Curator with no
// Embedder simply stores no vectors.
type Embedder interface {
	Embed(ctx context.Context, content string) ([]float32, error)
}

// MemVectorIndex is an in-memory VectorIndex for tests and non-pgvector runs.
type MemVectorIndex struct {
	mu   sync.Mutex
	rows map[string][]float32
}

// NewMemVectorIndex returns an empty MemVectorIndex.
func NewMemVectorIndex() *MemVectorIndex {
	return &MemVectorIndex{rows: make(map[string][]float32)}
}

// Upsert implements VectorIndex.
func (v *MemVectorIndex) Upsert(_ context.Context, memoryID string, vector []float32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	cp := make([]float32, len(vector))
	copy(cp, vector)
	v.rows[memoryID] = cp
	return nil
}

// Delete implements VectorIndex.
func (v *MemVectorIndex) Delete(_ context.Context, memoryID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.rows, memoryID)
	return nil
}

// Has implements VectorIndex.
func (v *MemVectorIndex) Has(_ context.Context, memoryID string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, ok := v.rows[memoryID]
	return ok, nil
}
