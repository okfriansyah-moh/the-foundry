package memory

import (
	"context"
	"testing"
	"time"
)

// TestProfileIsolation proves cross-profile read is impossible: a memory
// scoped to one profile is never returned to another, by any retrieval path.
func TestProfileIsolation(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	personal := &Curator{Proposer: staticProposer{candidates: []Candidate{
		{Content: "personal secret", ProfileScope: "personal", Confidence: 1, EvidenceRefs: []string{"ev-p"}},
	}}, Store: store, Now: fixedNow}
	org := &Curator{Proposer: staticProposer{candidates: []Candidate{
		{Content: "org fact", ProfileScope: "org", Confidence: 1, EvidenceRefs: []string{"ev-o"}},
	}}, Store: store, Now: fixedNow}

	pMem, err := personal.Curate(ctx, "personal", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := org.Curate(ctx, "org", nil); err != nil {
		t.Fatal(err)
	}

	// ListByProfile("org") must not include the personal memory.
	orgList, _ := store.ListByProfile(ctx, "org")
	for _, m := range orgList {
		if m.ProfileScope != "org" {
			t.Fatalf("cross-profile leak: org list contains %q", m.ProfileScope)
		}
	}

	// GetForProfile with the wrong profile must miss, even with the real ID.
	pID := pMem[0].ID
	if _, ok, _ := store.GetForProfile(ctx, "org", pID); ok {
		t.Fatal("cross-profile read: org retrieved a personal-scoped memory by ID")
	}
	if _, ok, _ := store.GetForProfile(ctx, "personal", pID); !ok {
		t.Fatal("owning profile could not read its own memory")
	}

	// Retrieve is per-profile too.
	got, _ := Retrieve(ctx, store, "personal", fixedNow())
	if len(got) != 1 || got[0].ProfileScope != "personal" {
		t.Fatalf("Retrieve leaked or dropped: %+v", got)
	}
}

// TestRetrieveDropsExpired proves TTL'd memories are filtered on retrieval.
func TestRetrieveDropsExpired(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	cur := &Curator{Proposer: staticProposer{candidates: []Candidate{
		{Content: "ephemeral", ProfileScope: "p", Confidence: 1, TTL: time.Hour, EvidenceRefs: []string{"ev"}},
	}}, Store: store, Now: fixedNow}
	if _, err := cur.Curate(ctx, "p", nil); err != nil {
		t.Fatal(err)
	}
	// Before expiry: present.
	if got, _ := Retrieve(ctx, store, "p", fixedNow().Add(30*time.Minute)); len(got) != 1 {
		t.Fatalf("pre-expiry retrieve should return 1, got %d", len(got))
	}
	// After expiry: dropped.
	if got, _ := Retrieve(ctx, store, "p", fixedNow().Add(2*time.Hour)); len(got) != 0 {
		t.Fatalf("post-expiry retrieve should return 0, got %d", len(got))
	}
}
