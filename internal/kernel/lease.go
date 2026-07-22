package kernel

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrLeaseHeld is returned by LeaseStore.Acquire when resource is currently
// held by a different holder and has not yet expired — the fencing
// conflict every mutating worktree activity must respect (Constitution C4:
// kernel owns leases and fencing).
var ErrLeaseHeld = errors.New("kernel: lease held by another holder")

// Lease is a granted fencing token for one resource.
type Lease struct {
	Resource  string
	Token     string
	Holder    string
	ExpiresAt time.Time
}

// LeaseStore grants and checks fencing tokens for a named resource.
// Acquiring the same (resource, holder) pair before expiry returns the
// same token (idempotent re-acquire); acquiring a resource held by a
// different, unexpired holder is a deterministic ErrLeaseHeld — retrying
// immediately cannot succeed, so callers must not blindly retry it.
type LeaseStore interface {
	Acquire(ctx context.Context, resource, holder string, ttl time.Duration) (Lease, error)
	// Check reports whether token is still the current, unexpired token
	// for resource — the fencing check every mutating activity must pass
	// before touching the resource.
	Check(ctx context.Context, resource, token string) (bool, error)
}

// newToken returns a random 128-bit fencing token, hex-encoded.
func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("kernel: generate lease token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// MemLeaseStore is an in-memory LeaseStore for tests and any run without a
// live Postgres.
type MemLeaseStore struct {
	mu     sync.Mutex
	leases map[string]Lease
	now    func() time.Time
}

// NewMemLeaseStore returns an empty MemLeaseStore.
func NewMemLeaseStore() *MemLeaseStore {
	return &MemLeaseStore{leases: make(map[string]Lease), now: time.Now}
}

// Acquire implements LeaseStore.
func (s *MemLeaseStore) Acquire(_ context.Context, resource, holder string, ttl time.Duration) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if existing, ok := s.leases[resource]; ok && now.Before(existing.ExpiresAt) {
		if existing.Holder != holder {
			return Lease{}, fmt.Errorf("%w: resource %q held by %q", ErrLeaseHeld, resource, existing.Holder)
		}
		existing.ExpiresAt = now.Add(ttl)
		s.leases[resource] = existing
		return existing, nil
	}

	token, err := newToken()
	if err != nil {
		return Lease{}, err
	}
	lease := Lease{Resource: resource, Token: token, Holder: holder, ExpiresAt: now.Add(ttl)}
	s.leases[resource] = lease
	return lease, nil
}

// Check implements LeaseStore.
func (s *MemLeaseStore) Check(_ context.Context, resource, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lease, ok := s.leases[resource]
	if !ok {
		return false, nil
	}
	return lease.Token == token && s.now().Before(lease.ExpiresAt), nil
}

// PGLeaseStore is the Postgres-backed LeaseStore (internal/db/migrations/00002).
type PGLeaseStore struct {
	db *sql.DB
}

// NewPGLeaseStore wraps an existing *sql.DB (typically opened via
// database/sql with the pgx driver, matching internal/provenance's
// PGRawStore pattern).
func NewPGLeaseStore(db *sql.DB) *PGLeaseStore { return &PGLeaseStore{db: db} }

// Acquire implements LeaseStore with an UPSERT guarded by the current
// holder/expiry, so a stale lease is reclaimed but a live one held by
// someone else is not.
func (s *PGLeaseStore) Acquire(ctx context.Context, resource, holder string, ttl time.Duration) (Lease, error) {
	token, err := newToken()
	if err != nil {
		return Lease{}, err
	}
	expiresAt := time.Now().Add(ttl)

	const q = `
INSERT INTO leases (resource, token, holder, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (resource) DO UPDATE SET
    token = CASE
        WHEN leases.holder = EXCLUDED.holder OR leases.expires_at < now() THEN EXCLUDED.token
        ELSE leases.token
    END,
    holder = CASE
        WHEN leases.holder = EXCLUDED.holder OR leases.expires_at < now() THEN EXCLUDED.holder
        ELSE leases.holder
    END,
    expires_at = CASE
        WHEN leases.holder = EXCLUDED.holder OR leases.expires_at < now() THEN EXCLUDED.expires_at
        ELSE leases.expires_at
    END
RETURNING token, holder, expires_at`

	var gotToken, gotHolder string
	var gotExpiresAt time.Time
	if err := s.db.QueryRowContext(ctx, q, resource, token, holder, expiresAt).Scan(&gotToken, &gotHolder, &gotExpiresAt); err != nil {
		return Lease{}, fmt.Errorf("kernel: acquire lease %s: %w", resource, err)
	}
	if gotHolder != holder {
		return Lease{}, fmt.Errorf("%w: resource %q held by %q", ErrLeaseHeld, resource, gotHolder)
	}
	return Lease{Resource: resource, Token: gotToken, Holder: gotHolder, ExpiresAt: gotExpiresAt}, nil
}

// Check implements LeaseStore.
func (s *PGLeaseStore) Check(ctx context.Context, resource, token string) (bool, error) {
	const q = `SELECT token, expires_at FROM leases WHERE resource = $1`
	var gotToken string
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx, q, resource).Scan(&gotToken, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kernel: check lease %s: %w", resource, err)
	}
	return gotToken == token && time.Now().Before(expiresAt), nil
}
