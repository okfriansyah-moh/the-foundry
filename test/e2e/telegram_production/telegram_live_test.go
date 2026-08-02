package telegramproduction_test

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

func TestDurableDraftConsumeBinding(t *testing.T) {
	s := notify.NewMemoryDraftStore()
	now := time.Now().UTC()
	_ = s.UpsertBinding(context.Background(), notify.ChatBinding{ChatID: "1", PrincipalID: "p"})
	_ = s.PutDraft(context.Background(), notify.DurableDraft{
		ID: "d", ChatID: "1", PrincipalID: "p", Kind: notify.DraftKindIdea,
		NonceHash: notify.HashNonce("n"), ExpiresAt: now.Add(time.Minute),
	})
	if _, err := s.ConsumeDraft(context.Background(), notify.HashNonce("n"), "1", "p", now); err != nil {
		t.Fatal(err)
	}
}
