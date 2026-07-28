package memory

import (
	"context"
	"testing"
)

// TestDeleteDerivedFrom_CascadesToMemoryAndVectors is Task 76's core
// acceptance: deleting a source evidence ref deletes every derived memory AND
// its vector-index entry. Derived knowledge never outlives its source.
func TestDeleteDerivedFrom_CascadesToMemoryAndVectors(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	vectors := NewMemVectorIndex()
	cur := &Curator{
		Proposer: staticProposer{candidates: []Candidate{
			{Content: "derived from ev1", ProfileScope: "p", Confidence: 1, EvidenceRefs: []string{"ev1"}},
			{Content: "also from ev1 and ev2", ProfileScope: "p", Confidence: 1, EvidenceRefs: []string{"ev1", "ev2"}},
			{Content: "only from ev2", ProfileScope: "p", Confidence: 1, EvidenceRefs: []string{"ev2"}},
		}},
		Store:    store,
		Vectors:  vectors,
		Embedder: fixedEmbedder{},
		Now:      fixedNow,
	}
	stored, err := cur.Curate(ctx, "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("want 3 stored memories, got %d", len(stored))
	}
	// All three have vectors indexed.
	for _, m := range stored {
		if has, _ := vectors.Has(ctx, m.ID); !has {
			t.Fatalf("memory %q missing vector after curate", m.ID)
		}
	}

	// Deleting ev1 removes the two memories derived (partly) from it, and
	// their vectors — but leaves the ev2-only memory intact.
	deleted, err := DeleteDerivedFrom(ctx, store, vectors, "ev1")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 {
		t.Fatalf("want 2 memories deleted by ev1, got %d: %v", len(deleted), deleted)
	}
	for _, id := range deleted {
		if has, _ := vectors.Has(ctx, id); has {
			t.Fatalf("vector for deleted memory %q survived (delete-with-source violated)", id)
		}
		if _, ok, _ := store.GetForProfile(ctx, "p", id); ok {
			t.Fatalf("deleted memory %q still retrievable", id)
		}
	}

	// The ev2-only memory remains, with its vector.
	remaining, _ := store.ListByProfile(ctx, "p")
	if len(remaining) != 1 {
		t.Fatalf("want 1 remaining memory (ev2-only), got %d", len(remaining))
	}
	if has, _ := vectors.Has(ctx, remaining[0].ID); !has {
		t.Fatal("surviving memory lost its vector")
	}
}
