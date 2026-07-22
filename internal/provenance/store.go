package provenance

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver registration
)

// ErrNotFound is returned by a RawStore.Load implementation when planID
// has no row.
var ErrNotFound = errors.New("provenance: approved plan not found")

// RawStore is the storage seam Store wraps with mandatory signature
// verification. Implementations do no cryptographic work themselves —
// PGRawStore is a thin parameterized-SQL layer, MemRawStore is an
// in-memory fake usable without a live Postgres (docs/PLAN.md Task 8 Step
// 6 / "no live Docker" test constraint).
type RawStore interface {
	Insert(ctx context.Context, a *ApprovedPlan) error
	Load(ctx context.Context, planID string) (*ApprovedPlan, error)
}

// Store is the kernel-facing API (docs/PLAN.md Task 8 Step 5): every Load
// verifies the Ed25519 signature before returning, and every Insert
// refuses to persist a row that doesn't already carry a valid signature —
// there is no code path that stores or serves an ApprovedPlan without a
// passing Verify.
type Store struct {
	raw RawStore
	pub ed25519.PublicKey
}

// NewStore builds a Store over raw, verifying against pub.
func NewStore(raw RawStore, pub ed25519.PublicKey) *Store {
	return &Store{raw: raw, pub: pub}
}

// Insert verifies a's signature, then persists it. An unsigned or
// forged ApprovedPlan is rejected here — it never reaches the store.
func (s *Store) Insert(ctx context.Context, a *ApprovedPlan) error {
	if err := Verify(s.pub, a); err != nil {
		return fmt.Errorf("provenance: refusing to insert unverified ApprovedPlan: %w", err)
	}
	return s.raw.Insert(ctx, a)
}

// Load fetches the ApprovedPlan for planID and verifies its signature
// before returning it. Tampering — whether a corrupted DB row or a
// forged/edited artifact — surfaces as an error here, never as a silently
// altered result (Constitution C7).
func (s *Store) Load(ctx context.Context, planID string) (*ApprovedPlan, error) {
	a, err := s.raw.Load(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := Verify(s.pub, a); err != nil {
		return nil, fmt.Errorf("provenance: tampered ApprovedPlan %s: %w", planID, err)
	}
	return a, nil
}

// MemRawStore is an in-memory RawStore for tests and for any run without a
// live Postgres. It stores the same JSON bytes a real DB row would hold,
// so tampering tests can flip a byte in exactly the representation that
// would be read back from disk.
type MemRawStore struct {
	rows map[string][]byte
}

// NewMemRawStore returns an empty MemRawStore.
func NewMemRawStore() *MemRawStore {
	return &MemRawStore{rows: make(map[string][]byte)}
}

// Insert implements RawStore.
func (m *MemRawStore) Insert(_ context.Context, a *ApprovedPlan) error {
	if _, exists := m.rows[a.planID]; exists {
		return fmt.Errorf("provenance: approved plan %s already exists", a.planID)
	}
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("provenance: marshal approved plan: %w", err)
	}
	m.rows[a.planID] = data
	return nil
}

// Load implements RawStore.
func (m *MemRawStore) Load(_ context.Context, planID string) (*ApprovedPlan, error) {
	data, ok := m.rows[planID]
	if !ok {
		return nil, ErrNotFound
	}
	var a ApprovedPlan
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("provenance: unmarshal stored approved plan %s: %w", planID, err)
	}
	return &a, nil
}

// CorruptRow flips one byte in the raw stored bytes for planID, simulating
// DB-row tampering for tests. It is a no-op if planID has no row.
func (m *MemRawStore) CorruptRow(planID string, byteIndex int) {
	data, ok := m.rows[planID]
	if !ok || byteIndex < 0 || byteIndex >= len(data) {
		return
	}
	corrupted := append([]byte{}, data...)
	corrupted[byteIndex] ^= 0xFF
	m.rows[planID] = corrupted
}

// PGRawStore is the Postgres-backed RawStore
// (internal/db/migrations/00001_approved_plans.sql). All queries are parameterized —
// no string-built SQL, no injection surface.
type PGRawStore struct {
	db *sql.DB
}

// OpenPGRawStore opens a PGRawStore against dsn using the pgx
// database/sql driver, matching the pattern already used by
// cmd/foundry/doctor.go.
func OpenPGRawStore(dsn string) (*PGRawStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("provenance: open postgres: %w", err)
	}
	return &PGRawStore{db: db}, nil
}

// Close closes the underlying connection pool.
func (p *PGRawStore) Close() error { return p.db.Close() }

// Insert implements RawStore with a parameterized INSERT. Re-inserting an
// existing plan_id is a primary-key violation, not silently ignored —
// approved_plans is append-only.
func (p *PGRawStore) Insert(ctx context.Context, a *ApprovedPlan) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("provenance: marshal approved plan: %w", err)
	}
	const q = `
INSERT INTO approved_plans (plan_id, plan_digest, approved_at, expires_at, revoked, data)
VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := p.db.ExecContext(ctx, q, a.planID, a.planDigest, a.approvedAt, a.expiresAt, a.revoked, data); err != nil {
		return fmt.Errorf("provenance: insert approved plan %s: %w", a.planID, err)
	}
	return nil
}

// Load implements RawStore with a parameterized SELECT.
func (p *PGRawStore) Load(ctx context.Context, planID string) (*ApprovedPlan, error) {
	const q = `SELECT data FROM approved_plans WHERE plan_id = $1`
	var data []byte
	err := p.db.QueryRowContext(ctx, q, planID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("provenance: load approved plan %s: %w", planID, err)
	}
	var a ApprovedPlan
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("provenance: unmarshal stored approved plan %s: %w", planID, err)
	}
	return &a, nil
}
