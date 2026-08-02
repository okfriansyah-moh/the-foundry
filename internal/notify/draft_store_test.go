package notify

import (
	"context"
	"testing"
	"time"
)

func TestMemoryDraftStore_ConsumeOnce(t *testing.T) {
	s := NewMemoryDraftStore()
	now := time.Now().UTC()
	d := DurableDraft{
		ID: "d1", ChatID: "c1", PrincipalID: "p1", Kind: DraftKindIdea,
		ContentHash: "h", NonceHash: HashNonce("n1"), ExpiresAt: now.Add(time.Minute),
	}
	if err := s.PutDraft(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	got, err := s.ConsumeDraft(context.Background(), HashNonce("n1"), "c1", "p1", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "d1" {
		t.Fatalf("id=%s", got.ID)
	}
	if _, err := s.ConsumeDraft(context.Background(), HashNonce("n1"), "c1", "p1", now); err != ErrDraftNotFound && err != ErrDraftUsed {
		t.Fatalf("second consume: %v", err)
	}
}

func TestMemoryDraftStore_ChatMismatch(t *testing.T) {
	s := NewMemoryDraftStore()
	now := time.Now().UTC()
	_ = s.PutDraft(context.Background(), DurableDraft{
		ID: "d1", ChatID: "c1", PrincipalID: "p1", Kind: DraftKindIdea,
		NonceHash: HashNonce("n1"), ExpiresAt: now.Add(time.Minute),
	})
	if _, err := s.ConsumeDraft(context.Background(), HashNonce("n1"), "c2", "p1", now); err != ErrDraftMismatch {
		t.Fatalf("got %v", err)
	}
}
