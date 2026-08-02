package notify

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// docs/PLAN.md Task 145 (INT-08): durable draft/nonce/binding store.

// ErrDraftNotFound / ErrDraftExpired / ErrDraftUsed are fail-closed draft outcomes.
var (
	ErrDraftNotFound = errors.New("notify: draft not found")
	ErrDraftExpired  = errors.New("notify: draft expired")
	ErrDraftUsed     = errors.New("notify: draft already used")
	ErrDraftMismatch = errors.New("notify: draft binding mismatch")
)

// DraftKind is IDEA or MOCKUP.
type DraftKind string

const (
	DraftKindIdea   DraftKind = "IDEA"
	DraftKindMockup DraftKind = "MOCKUP"
)

// DurableDraft is one persisted Telegram draft awaiting /confirm.
type DurableDraft struct {
	ID              string
	ChatID          string
	PrincipalID     string
	Kind            DraftKind
	ContentHash     string
	ContentText     string
	ArtifactRef     string
	ArtifactDigest  string
	BudgetUSD       float64
	NonceHash       string
	ExpiresAt       time.Time
	UsedAt          time.Time
	ConfirmedRun    string
	ConfirmedMission string
	CreatedAt       time.Time
}

// ChatBinding maps a Telegram chat to an authenticated principal/profile.
type ChatBinding struct {
	ChatID      string
	PrincipalID string
	ProfileID   string
	BotID       string
}

// DraftStore persists drafts, bindings and command audit.
type DraftStore interface {
	UpsertBinding(ctx context.Context, b ChatBinding) error
	GetBinding(ctx context.Context, chatID string) (ChatBinding, error)
	PutDraft(ctx context.Context, d DurableDraft) error
	GetDraft(ctx context.Context, draftID string) (DurableDraft, error)
	ConsumeDraft(ctx context.Context, nonceHash, chatID, principalID string, now time.Time) (DurableDraft, error)
	AuditCommand(ctx context.Context, chatID, principalID, command string, updateID int64, result string) error
}

// MemoryDraftStore is the in-process DraftStore for tests.
type MemoryDraftStore struct {
	bindings map[string]ChatBinding
	drafts   map[string]DurableDraft
	byNonce  map[string]string
}

// NewMemoryDraftStore constructs an empty MemoryDraftStore.
func NewMemoryDraftStore() *MemoryDraftStore {
	return &MemoryDraftStore{
		bindings: map[string]ChatBinding{},
		drafts:   map[string]DurableDraft{},
		byNonce:  map[string]string{},
	}
}

func (m *MemoryDraftStore) UpsertBinding(_ context.Context, b ChatBinding) error {
	m.bindings[b.ChatID] = b
	return nil
}

func (m *MemoryDraftStore) GetBinding(_ context.Context, chatID string) (ChatBinding, error) {
	b, ok := m.bindings[chatID]
	if !ok {
		return ChatBinding{}, ErrUnknownChat
	}
	return b, nil
}

func (m *MemoryDraftStore) PutDraft(_ context.Context, d DurableDraft) error {
	m.drafts[d.ID] = d
	if d.NonceHash != "" {
		m.byNonce[d.NonceHash] = d.ID
	}
	return nil
}

func (m *MemoryDraftStore) GetDraft(_ context.Context, draftID string) (DurableDraft, error) {
	d, ok := m.drafts[draftID]
	if !ok {
		return DurableDraft{}, ErrDraftNotFound
	}
	return d, nil
}

func (m *MemoryDraftStore) ConsumeDraft(_ context.Context, nonceHash, chatID, principalID string, now time.Time) (DurableDraft, error) {
	id, ok := m.byNonce[nonceHash]
	if !ok {
		return DurableDraft{}, ErrDraftNotFound
	}
	d := m.drafts[id]
	if !d.UsedAt.IsZero() {
		return DurableDraft{}, ErrDraftUsed
	}
	if now.After(d.ExpiresAt) {
		return DurableDraft{}, ErrDraftExpired
	}
	if d.ChatID != chatID || d.PrincipalID != principalID {
		return DurableDraft{}, ErrDraftMismatch
	}
	d.UsedAt = now
	m.drafts[id] = d
	delete(m.byNonce, nonceHash)
	return d, nil
}

func (m *MemoryDraftStore) AuditCommand(context.Context, string, string, string, int64, string) error {
	return nil
}

// PGDraftStore is the Postgres-backed DraftStore (migration 00039).
type PGDraftStore struct {
	DB *sql.DB
}

func (p *PGDraftStore) UpsertBinding(ctx context.Context, b ChatBinding) error {
	_, err := p.DB.ExecContext(ctx, `
INSERT INTO telegram_chat_bindings (chat_id, principal_id, profile_id, bot_id, updated_at)
VALUES ($1,$2,$3,$4,now())
ON CONFLICT (chat_id) DO UPDATE SET
  principal_id=EXCLUDED.principal_id,
  profile_id=EXCLUDED.profile_id,
  bot_id=EXCLUDED.bot_id,
  updated_at=now()`, b.ChatID, b.PrincipalID, b.ProfileID, b.BotID)
	if err != nil {
		return fmt.Errorf("notify: upsert chat binding: %w", err)
	}
	return nil
}

func (p *PGDraftStore) GetBinding(ctx context.Context, chatID string) (ChatBinding, error) {
	var b ChatBinding
	err := p.DB.QueryRowContext(ctx, `
SELECT chat_id, principal_id, profile_id, bot_id
FROM telegram_chat_bindings WHERE chat_id=$1`, chatID).
		Scan(&b.ChatID, &b.PrincipalID, &b.ProfileID, &b.BotID)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatBinding{}, ErrUnknownChat
	}
	if err != nil {
		return ChatBinding{}, fmt.Errorf("notify: get chat binding: %w", err)
	}
	return b, nil
}

func (p *PGDraftStore) PutDraft(ctx context.Context, d DurableDraft) error {
	_, err := p.DB.ExecContext(ctx, `
INSERT INTO telegram_drafts (
  draft_id, chat_id, principal_id, kind, content_hash, content_text,
  artifact_ref, artifact_digest, budget_usd, nonce_hash, expires_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())`,
		d.ID, d.ChatID, d.PrincipalID, string(d.Kind), d.ContentHash, d.ContentText,
		d.ArtifactRef, d.ArtifactDigest, d.BudgetUSD, d.NonceHash, d.ExpiresAt)
	if err != nil {
		return fmt.Errorf("notify: put draft: %w", err)
	}
	return nil
}

func (p *PGDraftStore) GetDraft(ctx context.Context, draftID string) (DurableDraft, error) {
	var d DurableDraft
	var kind string
	var used sql.NullTime
	err := p.DB.QueryRowContext(ctx, `
SELECT draft_id, chat_id, principal_id, kind, content_hash, content_text,
       artifact_ref, artifact_digest, budget_usd, nonce_hash, expires_at, used_at,
       confirmed_run, confirmed_mission, created_at
FROM telegram_drafts WHERE draft_id=$1`, draftID).Scan(
		&d.ID, &d.ChatID, &d.PrincipalID, &kind, &d.ContentHash, &d.ContentText,
		&d.ArtifactRef, &d.ArtifactDigest, &d.BudgetUSD, &d.NonceHash, &d.ExpiresAt, &used,
		&d.ConfirmedRun, &d.ConfirmedMission, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DurableDraft{}, ErrDraftNotFound
	}
	if err != nil {
		return DurableDraft{}, fmt.Errorf("notify: get draft: %w", err)
	}
	d.Kind = DraftKind(kind)
	if used.Valid {
		d.UsedAt = used.Time
	}
	return d, nil
}

func (p *PGDraftStore) ConsumeDraft(ctx context.Context, nonceHash, chatID, principalID string, now time.Time) (DurableDraft, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return DurableDraft{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var d DurableDraft
	var kind string
	var used sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT draft_id, chat_id, principal_id, kind, content_hash, content_text,
       artifact_ref, artifact_digest, budget_usd, nonce_hash, expires_at, used_at,
       confirmed_run, confirmed_mission, created_at
FROM telegram_drafts WHERE nonce_hash=$1 FOR UPDATE`, nonceHash).Scan(
		&d.ID, &d.ChatID, &d.PrincipalID, &kind, &d.ContentHash, &d.ContentText,
		&d.ArtifactRef, &d.ArtifactDigest, &d.BudgetUSD, &d.NonceHash, &d.ExpiresAt, &used,
		&d.ConfirmedRun, &d.ConfirmedMission, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DurableDraft{}, ErrDraftNotFound
	}
	if err != nil {
		return DurableDraft{}, fmt.Errorf("notify: consume draft: %w", err)
	}
	d.Kind = DraftKind(kind)
	if used.Valid {
		return DurableDraft{}, ErrDraftUsed
	}
	if now.After(d.ExpiresAt) {
		return DurableDraft{}, ErrDraftExpired
	}
	if d.ChatID != chatID || d.PrincipalID != principalID {
		return DurableDraft{}, ErrDraftMismatch
	}
	if _, err := tx.ExecContext(ctx, `UPDATE telegram_drafts SET used_at=$2 WHERE draft_id=$1`, d.ID, now); err != nil {
		return DurableDraft{}, err
	}
	if err := tx.Commit(); err != nil {
		return DurableDraft{}, err
	}
	d.UsedAt = now
	return d, nil
}

func (p *PGDraftStore) AuditCommand(ctx context.Context, chatID, principalID, command string, updateID int64, result string) error {
	_, err := p.DB.ExecContext(ctx, `
INSERT INTO telegram_command_audit (chat_id, principal_id, command, update_id, result)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT DO NOTHING`, chatID, principalID, command, updateID, result)
	if err != nil {
		return fmt.Errorf("notify: audit command: %w", err)
	}
	return nil
}

// HashNonce returns sha256 hex of a nonce string for durable storage.
func HashNonce(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}
