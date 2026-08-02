package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// EnvelopeStore persists immutable execution envelopes before Temporal start
// (docs/PLAN.md Task 141). Records are append-only; the only post-insert
// mutation is revocation metadata that never changes authority fields.
type EnvelopeStore interface {
	Insert(ctx context.Context, env *ExecutionEnvelope) error
	LoadByID(ctx context.Context, envelopeID string) (*ExecutionEnvelope, bool, error)
	LoadByDigest(ctx context.Context, digest string) (*ExecutionEnvelope, bool, error)
	Revoke(ctx context.Context, envelopeID, reason string, at time.Time) error
}

// MemEnvelopeStore is an in-memory EnvelopeStore for tests.
type MemEnvelopeStore struct {
	mu      sync.Mutex
	byID    map[string]*storedEnvelope
	byDigest map[string]string
}

type storedEnvelope struct {
	env     ExecutionEnvelope
	revoked bool
	reason  string
	at      time.Time
}

// NewMemEnvelopeStore returns an empty in-memory envelope store.
func NewMemEnvelopeStore() *MemEnvelopeStore {
	return &MemEnvelopeStore{
		byID:     make(map[string]*storedEnvelope),
		byDigest: make(map[string]string),
	}
}

// Insert implements EnvelopeStore.
func (s *MemEnvelopeStore) Insert(_ context.Context, env *ExecutionEnvelope) error {
	if env == nil {
		return fmt.Errorf("kernel: insert nil execution envelope")
	}
	if err := env.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[env.EnvelopeID]; ok {
		return fmt.Errorf("kernel: execution envelope %s already exists", env.EnvelopeID)
	}
	if id, ok := s.byDigest[env.EnvelopeDigest]; ok {
		return fmt.Errorf("kernel: execution envelope digest %s already stored as %s", env.EnvelopeDigest, id)
	}
	clone := *env
	s.byID[env.EnvelopeID] = &storedEnvelope{env: clone}
	s.byDigest[env.EnvelopeDigest] = env.EnvelopeID
	return nil
}

// LoadByID implements EnvelopeStore.
func (s *MemEnvelopeStore) LoadByID(_ context.Context, envelopeID string) (*ExecutionEnvelope, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.byID[envelopeID]
	if !ok {
		return nil, false, ErrEnvelopeNotFound
	}
	clone := row.env
	return &clone, row.revoked, nil
}

// LoadByDigest implements EnvelopeStore.
func (s *MemEnvelopeStore) LoadByDigest(_ context.Context, digest string) (*ExecutionEnvelope, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byDigest[digest]
	if !ok {
		return nil, false, ErrEnvelopeNotFound
	}
	row := s.byID[id]
	clone := row.env
	return &clone, row.revoked, nil
}

// Revoke implements EnvelopeStore.
func (s *MemEnvelopeStore) Revoke(_ context.Context, envelopeID, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.byID[envelopeID]
	if !ok {
		return ErrEnvelopeNotFound
	}
	row.revoked = true
	row.reason = reason
	row.at = at.UTC()
	return nil
}

// PGEnvelopeStore is the Postgres-backed EnvelopeStore
// (internal/db/migrations/00035_execution_envelopes.sql).
type PGEnvelopeStore struct {
	db *sql.DB
}

// NewPGEnvelopeStore wraps an existing *sql.DB.
func NewPGEnvelopeStore(db *sql.DB) *PGEnvelopeStore { return &PGEnvelopeStore{db: db} }

// Insert implements EnvelopeStore. Digests are unique; duplicate insert fails.
func (s *PGEnvelopeStore) Insert(ctx context.Context, env *ExecutionEnvelope) error {
	if env == nil {
		return fmt.Errorf("kernel: insert nil execution envelope")
	}
	if err := env.Validate(); err != nil {
		return err
	}
	payload, err := env.CanonicalJSON()
	if err != nil {
		return err
	}
	var expires any
	if env.Validity.ExpiresAt != nil {
		expires = env.Validity.ExpiresAt.UTC()
	}
	const q = `
INSERT INTO execution_envelopes (
    envelope_id, envelope_digest, schema_version, payload,
    approved_plan_id, plan_digest, mission_id, portfolio_id, profile_id,
    organization_id, principal_id, policy_digest, approval_ref,
    issued_at, expires_at
) VALUES (
    $1,$2,$3,$4,
    $5,$6,$7,$8,$9,
    $10,$11,$12,$13,
    $14,$15
)`
	_, err = s.db.ExecContext(ctx, q,
		env.EnvelopeID, env.EnvelopeDigest, env.SchemaVersion, payload,
		env.Plan.ApprovedPlanID, env.Plan.PlanDigest,
		env.Ownership.MissionID, env.Ownership.PortfolioID, env.Ownership.ProfileID,
		env.Ownership.OrganizationID, env.Ownership.PrincipalID,
		env.Policy.PolicyDigest, env.Plan.ApprovalRef,
		env.Validity.IssuedAt.UTC(), expires,
	)
	if err != nil {
		return fmt.Errorf("kernel: insert execution envelope %s: %w", env.EnvelopeID, err)
	}
	return nil
}

// LoadByID implements EnvelopeStore.
func (s *PGEnvelopeStore) LoadByID(ctx context.Context, envelopeID string) (*ExecutionEnvelope, bool, error) {
	const q = `
SELECT payload, revoked
FROM execution_envelopes
WHERE envelope_id = $1`
	return s.load(ctx, q, envelopeID)
}

// LoadByDigest implements EnvelopeStore.
func (s *PGEnvelopeStore) LoadByDigest(ctx context.Context, digest string) (*ExecutionEnvelope, bool, error) {
	const q = `
SELECT payload, revoked
FROM execution_envelopes
WHERE envelope_digest = $1`
	return s.load(ctx, q, digest)
}

func (s *PGEnvelopeStore) load(ctx context.Context, q, key string) (*ExecutionEnvelope, bool, error) {
	var payload []byte
	var revoked bool
	err := s.db.QueryRowContext(ctx, q, key).Scan(&payload, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrEnvelopeNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("kernel: load execution envelope %s: %w", key, err)
	}
	var env ExecutionEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, false, fmt.Errorf("kernel: decode execution envelope %s: %w", key, err)
	}
	if err := env.Validate(); err != nil {
		return nil, false, fmt.Errorf("%w: stored envelope %s: %v", ErrEnvelopeTampered, key, err)
	}
	return &env, revoked, nil
}

// Revoke implements EnvelopeStore.
func (s *PGEnvelopeStore) Revoke(ctx context.Context, envelopeID, reason string, at time.Time) error {
	const q = `
UPDATE execution_envelopes
SET revoked = TRUE, revoked_at = $2, revocation_reason = $3
WHERE envelope_id = $1`
	res, err := s.db.ExecContext(ctx, q, envelopeID, at.UTC(), reason)
	if err != nil {
		return fmt.Errorf("kernel: revoke execution envelope %s: %w", envelopeID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("kernel: revoke execution envelope %s rows: %w", envelopeID, err)
	}
	if n == 0 {
		return ErrEnvelopeNotFound
	}
	return nil
}

// LoadAndVerifyEnvelope loads by digest and refuses revoked/expired/tampered.
func LoadAndVerifyEnvelope(ctx context.Context, store EnvelopeStore, digest string, now time.Time) (*ExecutionEnvelope, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: missing envelope store", ErrEnvelopeRefused)
	}
	if digest == "" {
		return nil, fmt.Errorf("%w: empty envelope digest", ErrEnvelopeRefused)
	}
	env, revoked, err := store.LoadByDigest(ctx, digest)
	if err != nil {
		return nil, err
	}
	if err := env.VerifyUsable(now, revoked); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvelopeRefused, err)
	}
	return env, nil
}
