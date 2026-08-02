package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// docs/PLAN.md Task 145: attachment preservation for mockup drafts.

// AttachmentStore persists original Telegram media bytes unchanged.
type AttachmentStore interface {
	Put(ctx context.Context, key string, body []byte) (digest string, err error)
}

// MemoryAttachmentStore is an in-memory AttachmentStore for tests.
type MemoryAttachmentStore struct {
	Objects map[string][]byte
}

// Put implements AttachmentStore.
func (m *MemoryAttachmentStore) Put(_ context.Context, key string, body []byte) (string, error) {
	if m.Objects == nil {
		m.Objects = map[string][]byte{}
	}
	cp := append([]byte(nil), body...)
	m.Objects[key] = cp
	sum := sha256.Sum256(cp)
	return hex.EncodeToString(sum[:]), nil
}

// MockupDraftInput is the untrusted caption + preserved artifact.
type MockupDraftInput struct {
	ChatID      string
	PrincipalID string
	Caption     string
	Filename    string
	Bytes       []byte
	BudgetUSD   float64
}

// CreateMockupDraft stores original bytes, digests them, and returns a DurableDraft.
func CreateMockupDraft(ctx context.Context, store AttachmentStore, in MockupDraftInput, now time.Time, nonce string) (DurableDraft, error) {
	if store == nil {
		return DurableDraft{}, fmt.Errorf("notify: attachment store required")
	}
	if len(in.Bytes) == 0 {
		return DurableDraft{}, fmt.Errorf("notify: empty attachment refused")
	}
	key := fmt.Sprintf("telegram/%s/%s", in.ChatID, in.Filename)
	digest, err := store.Put(ctx, key, in.Bytes)
	if err != nil {
		return DurableDraft{}, err
	}
	capHash := sha256.Sum256([]byte(in.Caption))
	return DurableDraft{
		ID:             "draft-mockup-" + digest[:16],
		ChatID:         in.ChatID,
		PrincipalID:    in.PrincipalID,
		Kind:           DraftKindMockup,
		ContentHash:    hex.EncodeToString(capHash[:]),
		ContentText:    in.Caption, // untrusted context only
		ArtifactRef:    key,
		ArtifactDigest: digest,
		BudgetUSD:      in.BudgetUSD,
		NonceHash:      HashNonce(nonce),
		ExpiresAt:      now.Add(DraftTTL),
		CreatedAt:      now,
	}, nil
}
