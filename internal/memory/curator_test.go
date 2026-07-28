package memory

import (
	"context"
	"testing"
	"time"
)

// staticProposer is the cassette/static stand-in for the LLM proposer, so
// tests are deterministic and make no network call.
type staticProposer struct{ candidates []Candidate }

func (p staticProposer) Propose(_ context.Context, _ string, _ []EvidenceInput) ([]Candidate, error) {
	return p.candidates, nil
}

// fixedEmbedder returns a deterministic vector from content length.
type fixedEmbedder struct{}

func (fixedEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	return []float32{float32(len(content))}, nil
}

func fixedNow() time.Time { return time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC) }

func TestCurate_StoresWithProvenance(t *testing.T) {
	store := NewMemStore()
	cur := &Curator{
		Proposer: staticProposer{candidates: []Candidate{
			{Content: "Use JWT for auth", Kind: "convention", ProfileScope: "personal", Confidence: 0.9, EvidenceRefs: []string{"ev1"}},
		}},
		Store: store,
		Now:   fixedNow,
	}
	got, err := cur.Curate(context.Background(), "personal", []EvidenceInput{{Ref: "ev1", Text: "..."}})
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 memory, got %d", len(got))
	}
	if got[0].ProfileScope != "personal" || len(got[0].EvidenceRefs) != 1 {
		t.Fatalf("memory not provenance-stamped/scoped: %+v", got[0])
	}
}

func TestCurate_DedupesAndMergesEvidence(t *testing.T) {
	store := NewMemStore()
	// Two candidates normalize to the same content: merged, not duplicated,
	// with the union of evidence refs and the higher confidence.
	cur := &Curator{
		Proposer: staticProposer{candidates: []Candidate{
			{Content: "Use JWT for auth", ProfileScope: "personal", Confidence: 0.5, EvidenceRefs: []string{"ev1"}},
			{Content: "  use   JWT for AUTH ", ProfileScope: "personal", Confidence: 0.8, EvidenceRefs: []string{"ev2"}},
		}},
		Store: store,
		Now:   fixedNow,
	}
	got, err := cur.Curate(context.Background(), "personal", nil)
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 merged memory, got %d", len(got))
	}
	if len(got[0].EvidenceRefs) != 2 {
		t.Fatalf("want merged evidence refs [ev1 ev2], got %v", got[0].EvidenceRefs)
	}
	if got[0].Confidence != 0.8 {
		t.Fatalf("want max confidence 0.8, got %v", got[0].Confidence)
	}
}

func TestCurate_MergesAcrossCalls(t *testing.T) {
	store := NewMemStore()
	newCurator := func(cand Candidate) *Curator {
		return &Curator{Proposer: staticProposer{candidates: []Candidate{cand}}, Store: store, Now: fixedNow}
	}
	if _, err := newCurator(Candidate{Content: "X", ProfileScope: "p", Confidence: 0.3, EvidenceRefs: []string{"a"}}).Curate(context.Background(), "p", nil); err != nil {
		t.Fatal(err)
	}
	got, err := newCurator(Candidate{Content: "X", ProfileScope: "p", Confidence: 0.9, EvidenceRefs: []string{"b"}}).Curate(context.Background(), "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].EvidenceRefs) != 2 || got[0].Confidence != 0.9 {
		t.Fatalf("second Curate did not merge into existing memory: %+v", got[0])
	}
	all, _ := store.ListByProfile(context.Background(), "p")
	if len(all) != 1 {
		t.Fatalf("expected 1 stored memory after merge, got %d", len(all))
	}
}

func TestCurate_RejectsCrossProfileWrite(t *testing.T) {
	cur := &Curator{
		Proposer: staticProposer{candidates: []Candidate{
			{Content: "leak", ProfileScope: "org", Confidence: 1, EvidenceRefs: []string{"ev"}},
		}},
		Store: NewMemStore(),
		Now:   fixedNow,
	}
	if _, err := cur.Curate(context.Background(), "personal", nil); err == nil {
		t.Fatal("expected cross-profile write to be refused")
	}
}

func TestCurate_RejectsMissingProvenance(t *testing.T) {
	cur := &Curator{
		Proposer: staticProposer{candidates: []Candidate{{Content: "x", ProfileScope: "p", Confidence: 1}}},
		Store:    NewMemStore(),
		Now:      fixedNow,
	}
	if _, err := cur.Curate(context.Background(), "p", nil); err == nil {
		t.Fatal("expected refusal of candidate with no evidence refs")
	}
}
