package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Store persists owned repository records.
type Store interface {
	Upsert(ctx context.Context, r Record) error
	Get(ctx context.Context, id string) (Record, error)
	GetByCanonicalURL(ctx context.Context, canonicalURL string) (Record, error)
}

// MemStore is an in-memory Store for tests.
type MemStore struct {
	mu    sync.Mutex
	byID  map[string]Record
	byURL map[string]string
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{byID: make(map[string]Record), byURL: make(map[string]string)}
}

// Upsert implements Store.
func (s *MemStore) Upsert(_ context.Context, r Record) error {
	norm, err := NormalizeCanonicalURL(r.CanonicalURL)
	if err != nil {
		return err
	}
	r.CanonicalURL = norm
	if err := r.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[r.ID] = r
	s.byURL[r.CanonicalURL] = r.ID
	return nil
}

// Get implements Store.
func (s *MemStore) Get(_ context.Context, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

// GetByCanonicalURL implements Store.
func (s *MemStore) GetByCanonicalURL(_ context.Context, canonicalURL string) (Record, error) {
	norm, err := NormalizeCanonicalURL(canonicalURL)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byURL[norm]
	if !ok {
		return Record{}, ErrNotFound
	}
	return s.byID[id], nil
}

// PGStore is the Postgres-backed Store (migration 00036).
type PGStore struct {
	db *sql.DB
}

// NewPGStore wraps db.
func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

// Upsert implements Store.
func (s *PGStore) Upsert(ctx context.Context, r Record) error {
	norm, err := NormalizeCanonicalURL(r.CanonicalURL)
	if err != nil {
		return err
	}
	r.CanonicalURL = norm
	if err := r.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	const q = `
INSERT INTO repository_registry (
  id, provider, canonical_url, alias, profile_id, organization_id,
  pinned_base_revision, default_target_branch, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
ON CONFLICT (id) DO UPDATE SET
  provider = EXCLUDED.provider,
  canonical_url = EXCLUDED.canonical_url,
  alias = EXCLUDED.alias,
  profile_id = EXCLUDED.profile_id,
  organization_id = EXCLUDED.organization_id,
  pinned_base_revision = EXCLUDED.pinned_base_revision,
  default_target_branch = EXCLUDED.default_target_branch,
  updated_at = EXCLUDED.updated_at`
	_, err = s.db.ExecContext(ctx, q,
		r.ID, r.Provider, r.CanonicalURL, r.Alias, r.ProfileID, r.OrganizationID,
		r.PinnedBaseRevision, r.DefaultTargetBranch, now,
	)
	if err != nil {
		return fmt.Errorf("repository: upsert %s: %w", r.ID, err)
	}
	return nil
}

// Get implements Store.
func (s *PGStore) Get(ctx context.Context, id string) (Record, error) {
	const q = `
SELECT id, provider, canonical_url, alias, profile_id, organization_id,
       pinned_base_revision, default_target_branch, created_at, updated_at
FROM repository_registry WHERE id = $1`
	var r Record
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&r.ID, &r.Provider, &r.CanonicalURL, &r.Alias, &r.ProfileID, &r.OrganizationID,
		&r.PinnedBaseRevision, &r.DefaultTargetBranch, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("repository: get %s: %w", id, err)
	}
	return r, nil
}

// GetByCanonicalURL implements Store.
func (s *PGStore) GetByCanonicalURL(ctx context.Context, canonicalURL string) (Record, error) {
	norm, err := NormalizeCanonicalURL(canonicalURL)
	if err != nil {
		return Record{}, err
	}
	const q = `
SELECT id, provider, canonical_url, alias, profile_id, organization_id,
       pinned_base_revision, default_target_branch, created_at, updated_at
FROM repository_registry WHERE canonical_url = $1`
	var r Record
	err = s.db.QueryRowContext(ctx, q, norm).Scan(
		&r.ID, &r.Provider, &r.CanonicalURL, &r.Alias, &r.ProfileID, &r.OrganizationID,
		&r.PinnedBaseRevision, &r.DefaultTargetBranch, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("repository: get by url: %w", err)
	}
	return r, nil
}
