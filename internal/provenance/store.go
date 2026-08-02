package provenance

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver registration
)

// ErrNotFound is returned by a RawStore.Load implementation when planID
// has no row.
var ErrNotFound = errors.New("provenance: approved plan not found")

// ErrPlanExpired is returned by Store.Load when an otherwise valid,
// signed ApprovedPlan's ExpiresAt has passed (docs/PLAN.md Task 24 /
// Constitution C7 rule 5: "plans expire ... execution re-checks
// revocation at every wave boundary"). It never leaks the plan's fields —
// callers get a closed door, not a partial read.
var ErrPlanExpired = errors.New("provenance: approved plan has expired")

// ErrPlanRevoked is returned by Store.Load when an otherwise valid,
// signed ApprovedPlan has been revoked (docs/PLAN.md Task 24). Revocation
// is enforced here — the single choke point every kernel/CLI read of an
// ApprovedPlan routes through — so it is immediate: there is no caching
// layer between this check and the signed row.
var ErrPlanRevoked = errors.New("provenance: approved plan has been revoked")

// RawStore is the storage seam Store wraps with mandatory signature
// verification. Implementations do no cryptographic work themselves —
// PGRawStore is a thin parameterized-SQL layer, MemRawStore is an
// in-memory fake usable without a live Postgres (docs/PLAN.md Task 8 Step
// 6 / "no live Docker" test constraint).
type RawStore interface {
	Insert(ctx context.Context, a *ApprovedPlan) error
	Load(ctx context.Context, planID string) (*ApprovedPlan, error)
	// Update overwrites the row for a.PlanID() with a's current fields —
	// the only mutation approved_plans ever needs post-insert is a
	// revocation (docs/PLAN.md Task 24); everything else about an
	// ApprovedPlan is immutable once signed.
	Update(ctx context.Context, a *ApprovedPlan) error
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

// Load fetches the ApprovedPlan for planID, verifies its signature, and
// enforces that it is neither expired nor revoked before returning it
// (Constitution C7 rule 5). Tampering — whether a corrupted DB row or a
// forged/edited artifact — surfaces as an error here, never as a silently
// altered result. This is the single path both `foundry plan verify` and
// the kernel's LoadApprovedPlan/RecheckApproval activities use, so
// expiry/revocation enforcement is immediate and uncached: a plan revoked
// one second ago fails the very next Load, with no staleness window.
func (s *Store) Load(ctx context.Context, planID string) (*ApprovedPlan, error) {
	a, err := s.raw.Load(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := Verify(s.pub, a); err != nil {
		return nil, fmt.Errorf("provenance: tampered ApprovedPlan %s: %w", planID, err)
	}
	if err := checkPlanOpen(planID, a); err != nil {
		return nil, err
	}
	return a, nil
}

// checkPlanOpen returns ErrPlanRevoked/ErrPlanExpired if a is no longer open
// to further action. Load and AddApprover both route through this single
// check so "is this plan still valid" can never drift between the two
// (docs/PLAN.md Task 25 secondary-review finding: AddApprover used to skip
// this, letting a WebAuthn-verified approval be recorded against an
// already-revoked/expired plan).
func checkPlanOpen(planID string, a *ApprovedPlan) error {
	if a.revoked {
		return fmt.Errorf("provenance: approved plan %s: %w", planID, ErrPlanRevoked)
	}
	if !a.expiresAt.IsZero() && time.Now().After(a.expiresAt) {
		return fmt.Errorf("provenance: approved plan %s: %w", planID, ErrPlanExpired)
	}
	return nil
}

// Revoke marks the ApprovedPlan for planID as revoked (Constitution C7),
// re-signs it under priv so the revocation itself is part of the
// tamper-evident artifact rather than a side-channel flag, and persists
// it via the underlying RawStore's Update. It loads through raw.Load
// directly (signature-verified but not expiry/revocation-gated) rather
// than through Store.Load, so an administrator can revoke a plan that has
// already expired or was already revoked (idempotent) without Load's own
// enforcement standing in the way.
func (s *Store) Revoke(ctx context.Context, planID string, priv ed25519.PrivateKey, revokedBy, reason string) (*ApprovedPlan, error) {
	a, err := s.raw.Load(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := Verify(s.pub, a); err != nil {
		return nil, fmt.Errorf("provenance: refusing to revoke tampered ApprovedPlan %s: %w", planID, err)
	}
	if err := Revoke(priv, a, revokedBy, reason); err != nil {
		return nil, err
	}
	if err := s.raw.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("provenance: persist revocation for %s: %w", planID, err)
	}
	return a, nil
}

// AddApprover records approver on the ApprovedPlan for planID, re-signs it
// under priv, and persists it via the underlying RawStore's Update
// (docs/PLAN.md Task 25 / Constitution C12). It loads via raw.Load
// directly rather than through Store.Load (like Revoke) so it can apply
// its own closed-door check — checkPlanOpen, the exact same
// revoked/expiresAt gate Store.Load enforces — before appending anything
// or re-signing, rather than inheriting Load's ErrNotFound-shaped
// semantics wholesale. A revoked or expired plan rejects here with
// ErrPlanRevoked/ErrPlanExpired: no approver is appended, no re-sign
// happens (docs/PLAN.md Task 25 secondary-review finding, closed).
func (s *Store) AddApprover(ctx context.Context, planID string, priv ed25519.PrivateKey, approver Approver) (*ApprovedPlan, error) {
	a, err := s.raw.Load(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := Verify(s.pub, a); err != nil {
		return nil, fmt.Errorf("provenance: refusing to add approver to tampered ApprovedPlan %s: %w", planID, err)
	}
	if err := checkPlanOpen(planID, a); err != nil {
		return nil, err
	}
	if err := AppendApprover(priv, a, approver); err != nil {
		return nil, err
	}
	if err := s.raw.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("provenance: persist approver for %s: %w", planID, err)
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

// Update implements RawStore by overwriting the existing row for
// a.PlanID(). It errors if planID has no row — Update is a mutation of an
// existing approval (a revocation), never an implicit insert.
func (m *MemRawStore) Update(_ context.Context, a *ApprovedPlan) error {
	if _, exists := m.rows[a.planID]; !exists {
		return ErrNotFound
	}
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("provenance: marshal approved plan: %w", err)
	}
	m.rows[a.planID] = data
	return nil
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

// NewPGRawStore wraps an existing *sql.DB (docs/PLAN.md Task 144 production
// intake shares the CLI connection pool with the intake store).
func NewPGRawStore(db *sql.DB) *PGRawStore {
	return &PGRawStore{db: db}
}

// OpenPGRawStore opens a PGRawStore against dsn using the pgx
// database/sql driver, matching the pattern already used by
// cmd/foundry/doctor.go.
func OpenPGRawStore(dsn string) (*PGRawStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("provenance: open postgres: %w", err)
	}
	return NewPGRawStore(db), nil
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

// Update implements RawStore with a parameterized UPDATE. It is the only
// way an approved_plans row ever changes post-insert (docs/PLAN.md Task
// 24's revocation); a no-op match (planID not found) is reported via
// ErrNotFound rather than silently succeeding.
func (p *PGRawStore) Update(ctx context.Context, a *ApprovedPlan) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("provenance: marshal approved plan: %w", err)
	}
	const q = `UPDATE approved_plans SET revoked = $2, data = $3 WHERE plan_id = $1`
	tag, err := p.db.ExecContext(ctx, q, a.planID, a.revoked, data)
	if err != nil {
		return fmt.Errorf("provenance: update approved plan %s: %w", a.planID, err)
	}
	if n, err := tag.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// DB returns the underlying *sql.DB connection pool, for callers (e.g.
// `foundry plan revoke`) that need to append a row to the audit_log hash
// chain (AppendAuditRow) in the same Postgres instance the approved_plans
// mutation was persisted to.
func (p *PGRawStore) DB() *sql.DB { return p.db }
