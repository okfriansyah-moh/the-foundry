package notify

import (
	"context"
	"testing"
	"time"
)

func TestCreateMockupDraft_PreservesDigest(t *testing.T) {
	store := &MemoryAttachmentStore{}
	body := []byte("%PDF-1.4 mock")
	d, err := CreateMockupDraft(context.Background(), store, MockupDraftInput{
		ChatID: "c1", PrincipalID: "p1", Caption: "wireframe", Filename: "a.pdf", Bytes: body, BudgetUSD: 25,
	}, time.Now().UTC(), "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != DraftKindMockup {
		t.Fatalf("kind=%s", d.Kind)
	}
	if d.ArtifactDigest == "" || d.ArtifactRef == "" {
		t.Fatal("missing artifact refs")
	}
	if string(store.Objects[d.ArtifactRef]) != string(body) {
		t.Fatal("bytes mutated")
	}
}
