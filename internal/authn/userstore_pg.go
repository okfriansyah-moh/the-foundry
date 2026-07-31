package authn

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// docs/PLAN.md Task 114 (INT-06): durable WebAuthn stores. foundryd previously
// wired authn.NewMemUserStore(), so every registered passkey, in-flight
// challenge and signature counter died on restart. These Postgres-backed stores
// change *where* credentials and sessions live, never *how strongly* they are
// verified (Constitution C12).

// ErrSignCountRegression is returned when a Put would lower a credential's
// stored signature counter — the clone-detection signal. It is a hard
// rejection, not a warning.
var ErrSignCountRegression = errors.New("authn: sign-count regression (possible cloned authenticator)")

// PGUserStore is the Postgres-backed UserStore
// (internal/db/migrations/00028_webauthn.sql). Credentials are unique per
// (principal, credential_id); a Put that lowers a credential's sign count is
// refused.
type PGUserStore struct {
	db *sql.DB
}

// NewPGUserStore wraps an existing *sql.DB.
func NewPGUserStore(db *sql.DB) *PGUserStore { return &PGUserStore{db: db} }

// Get returns principal's credential user, or a credential-less user when the
// principal has registered none yet (mirrors MemUserStore's behavior).
func (s *PGUserStore) Get(principal string) (*CredentialUser, error) {
	return s.get(context.Background(), principal)
}

func (s *PGUserStore) get(ctx context.Context, principal string) (*CredentialUser, error) {
	const q = `SELECT credential FROM webauthn_credentials WHERE principal = $1 ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, principal)
	if err != nil {
		return nil, fmt.Errorf("authn: load credentials for %s: %w", principal, err)
	}
	defer func() { _ = rows.Close() }()
	user := &CredentialUser{Principal: principal}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("authn: scan credential: %w", err)
		}
		var cred webauthn.Credential
		if err := json.Unmarshal(raw, &cred); err != nil {
			return nil, fmt.Errorf("authn: decode credential: %w", err)
		}
		user.Credentials = append(user.Credentials, cred)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authn: iterate credentials: %w", err)
	}
	return user, nil
}

// Put upserts every credential the user carries. A credential whose incoming
// sign count is below the stored one is refused (clone detection), and the row
// is left unchanged.
func (s *PGUserStore) Put(user *CredentialUser) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("authn: begin credential tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i := range user.Credentials {
		if err := putCredentialTx(ctx, tx, user.Principal, &user.Credentials[i]); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("authn: commit credentials: %w", err)
	}
	return nil
}

func putCredentialTx(ctx context.Context, tx *sql.Tx, principal string, cred *webauthn.Credential) error {
	credID := hex.EncodeToString(cred.ID)
	newCount := int64(cred.Authenticator.SignCount)

	var existing sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT sign_count FROM webauthn_credentials WHERE principal = $1 AND credential_id = $2`,
		principal, credID).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("authn: read existing sign count: %w", err)
	}
	if existing.Valid && newCount < existing.Int64 {
		// A regression means the counter went backwards — a cloned or replayed
		// authenticator. Refuse hard (C12 clone detection).
		return fmt.Errorf("%w: %s went from %d to %d", ErrSignCountRegression, credID, existing.Int64, newCount)
	}

	raw, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("authn: encode credential: %w", err)
	}
	aaguid := hex.EncodeToString(cred.Authenticator.AAGUID)
	const upsert = `
INSERT INTO webauthn_credentials
  (principal, credential_id, aaguid, sign_count, credential, created_at, last_used_at)
VALUES ($1,$2,$3,$4,$5, now(), now())
ON CONFLICT (principal, credential_id) DO UPDATE
SET sign_count = EXCLUDED.sign_count, credential = EXCLUDED.credential, last_used_at = now()`
	if _, err := tx.ExecContext(ctx, upsert, principal, credID, aaguid, newCount, raw); err != nil {
		return fmt.Errorf("authn: upsert credential %s: %w", credID, err)
	}
	return nil
}

// PGSessionStore is the Postgres-backed, single-use SessionStore. A challenge
// session survives a daemon restart; Pop deletes it on first read so a replay —
// including one across a restart — fails; expired rows are reaped.
type PGSessionStore struct {
	db  *sql.DB
	ttl time.Duration
}

// DefaultSessionTTL bounds how long a challenge session is valid.
const DefaultSessionTTL = 5 * time.Minute

// NewPGSessionStore wraps an existing *sql.DB with DefaultSessionTTL.
func NewPGSessionStore(db *sql.DB) *PGSessionStore {
	return &PGSessionStore{db: db, ttl: DefaultSessionTTL}
}

// Put records session under a fresh id with a bounded TTL.
func (s *PGSessionStore) Put(session webauthn.SessionData) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("authn: encode session: %w", err)
	}
	expires := time.Now().Add(s.ttl)
	if !session.Expires.IsZero() {
		expires = session.Expires
	}
	const q = `INSERT INTO webauthn_sessions (id, data, expires_at, created_at) VALUES ($1,$2,$3, now())`
	if _, err := s.db.ExecContext(context.Background(), q, id, raw, expires); err != nil {
		return "", fmt.Errorf("authn: persist session: %w", err)
	}
	return id, nil
}

// Pop returns and deletes the session for id (single-use). An unknown, already
// consumed, or expired id yields ok=false.
func (s *PGSessionStore) Pop(id string) (webauthn.SessionData, bool) {
	ctx := context.Background()
	// Best-effort reap of expired rows so they can never be consumed.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE expires_at <= now()`)
	const q = `DELETE FROM webauthn_sessions WHERE id = $1 AND expires_at > now() RETURNING data`
	var raw []byte
	if err := s.db.QueryRowContext(ctx, q, id).Scan(&raw); err != nil {
		return webauthn.SessionData{}, false
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(raw, &session); err != nil {
		return webauthn.SessionData{}, false
	}
	return session, true
}
