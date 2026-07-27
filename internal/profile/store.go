package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver registration
)

// ErrNotFound is returned when a lookup by ID finds no row.
var ErrNotFound = errors.New("profile: not found")

// ErrAlreadyExists is returned by Save when creating a profile whose ID is
// already taken, so idempotent callers (e.g.
// test/fixtures/seed_profiles.go) can detect "already seeded" without a
// database-specific error type.
var ErrAlreadyExists = errors.New("profile: already exists")

// RawStore is the storage seam Store wraps with mandatory config
// validation (mirrors internal/provenance's RawStore/MemRawStore split).
// MemRawStore is an in-memory fake for tests and any run without a live
// Postgres; PGRawStore is the real Postgres-backed implementation.
type RawStore interface {
	Insert(ctx context.Context, p *Profile) error
	Load(ctx context.Context, id string) (*Profile, error)
	List(ctx context.Context) ([]*Profile, error)
}

// Store is the profile-facing API: every Save validates the profile
// (including its config against profile.schema.json) before it ever reaches
// the raw store, so no invalid config is ever persisted.
type Store struct {
	raw RawStore
}

// NewStore builds a Store over raw.
func NewStore(raw RawStore) *Store {
	return &Store{raw: raw}
}

// Save validates p, then persists it. An invalid profile (bad kind, missing
// org_id, or a config that fails profile.schema.json) is rejected here — it
// never reaches the store.
func (s *Store) Save(ctx context.Context, p *Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return s.raw.Insert(ctx, p)
}

// Load fetches the profile for id.
func (s *Store) Load(ctx context.Context, id string) (*Profile, error) {
	return s.raw.Load(ctx, id)
}

// List returns every profile, ordered by ID.
func (s *Store) List(ctx context.Context) ([]*Profile, error) {
	return s.raw.List(ctx)
}

// MemRawStore is an in-memory RawStore for tests and for any run without a
// live Postgres. It stores the same JSON bytes a real DB row would hold.
type MemRawStore struct {
	rows map[string][]byte
}

// NewMemRawStore returns an empty MemRawStore.
func NewMemRawStore() *MemRawStore {
	return &MemRawStore{rows: make(map[string][]byte)}
}

// Insert implements RawStore.
func (m *MemRawStore) Insert(_ context.Context, p *Profile) error {
	if _, exists := m.rows[p.ID]; exists {
		return fmt.Errorf("%w: profile %s", ErrAlreadyExists, p.ID)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("profile: marshal %s: %w", p.ID, err)
	}
	m.rows[p.ID] = data
	return nil
}

// Load implements RawStore.
func (m *MemRawStore) Load(_ context.Context, id string) (*Profile, error) {
	data, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profile: unmarshal %s: %w", id, err)
	}
	return &p, nil
}

// List implements RawStore, sorted by ID for deterministic output.
func (m *MemRawStore) List(_ context.Context) ([]*Profile, error) {
	out := make([]*Profile, 0, len(m.rows))
	for id := range m.rows {
		p, err := m.Load(context.Background(), id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// PGRawStore is the Postgres-backed RawStore
// (internal/db/migrations/00005_profiles.sql). All queries are
// parameterized — no string-built SQL, no injection surface.
type PGRawStore struct {
	db *sql.DB
}

// OpenPGRawStore opens a PGRawStore against dsn using the pgx database/sql
// driver.
func OpenPGRawStore(dsn string) (*PGRawStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("profile: open postgres: %w", err)
	}
	return &PGRawStore{db: db}, nil
}

// Close closes the underlying connection pool.
func (p *PGRawStore) Close() error { return p.db.Close() }

// Insert implements RawStore with a parameterized INSERT.
func (p *PGRawStore) Insert(ctx context.Context, pr *Profile) error {
	const q = `
INSERT INTO profiles (id, name, kind, org_id, config, policy_digest)
VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := p.db.ExecContext(ctx, q, pr.ID, pr.Name, string(pr.Kind), pr.OrgID, []byte(pr.Config), pr.PolicyDigest); err != nil {
		return fmt.Errorf("profile: insert %s: %w", pr.ID, err)
	}
	return nil
}

// Load implements RawStore with a parameterized SELECT.
func (p *PGRawStore) Load(ctx context.Context, id string) (*Profile, error) {
	const q = `SELECT id, name, kind, org_id, config, policy_digest, created_at FROM profiles WHERE id = $1`
	pr := &Profile{}
	var config []byte
	err := p.db.QueryRowContext(ctx, q, id).Scan(&pr.ID, &pr.Name, &pr.Kind, &pr.OrgID, &config, &pr.PolicyDigest, &pr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("profile: load %s: %w", id, err)
	}
	pr.Config = config
	return pr, nil
}

// List implements RawStore with a parameterized SELECT, ordered by id.
func (p *PGRawStore) List(ctx context.Context) ([]*Profile, error) {
	const q = `SELECT id, name, kind, org_id, config, policy_digest, created_at FROM profiles ORDER BY id`
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("profile: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*Profile
	for rows.Next() {
		pr := &Profile{}
		var config []byte
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Kind, &pr.OrgID, &config, &pr.PolicyDigest, &pr.CreatedAt); err != nil {
			return nil, fmt.Errorf("profile: scan: %w", err)
		}
		pr.Config = config
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("profile: list: %w", err)
	}
	return out, nil
}
