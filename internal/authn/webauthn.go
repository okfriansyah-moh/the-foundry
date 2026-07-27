package authn

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// ErrUserNotFound is returned by a UserStore.Get implementation when
// principal has no registered WebAuthn state.
var ErrUserNotFound = errors.New("authn: webauthn user not found")

// CredentialUser is the minimal per-principal WebAuthn identity this
// package needs: a stable user handle plus the credentials registered so
// far. It implements webauthn.User so it can be passed straight into
// go-webauthn's ceremony functions.
type CredentialUser struct {
	Principal   string
	Credentials []webauthn.Credential
}

// WebAuthnID implements webauthn.User.
func (u *CredentialUser) WebAuthnID() []byte { return []byte(u.Principal) }

// WebAuthnName implements webauthn.User.
func (u *CredentialUser) WebAuthnName() string { return u.Principal }

// WebAuthnDisplayName implements webauthn.User.
func (u *CredentialUser) WebAuthnDisplayName() string { return u.Principal }

// WebAuthnCredentials implements webauthn.User.
func (u *CredentialUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// WebAuthnIcon implements webauthn.User.
func (u *CredentialUser) WebAuthnIcon() string { return "" }

// UserStore is the persistence seam Service uses for per-principal
// WebAuthn credentials. MemUserStore is an in-memory implementation for
// tests and any run without a live Postgres.
type UserStore interface {
	Get(principal string) (*CredentialUser, error)
	Put(user *CredentialUser) error
}

// MemUserStore is an in-memory UserStore for tests.
type MemUserStore struct {
	mu    sync.Mutex
	users map[string]*CredentialUser
}

// NewMemUserStore returns an empty MemUserStore.
func NewMemUserStore() *MemUserStore {
	return &MemUserStore{users: make(map[string]*CredentialUser)}
}

// Get implements UserStore.
func (s *MemUserStore) Get(principal string) (*CredentialUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[principal]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUserNotFound, principal)
	}
	cp := *u
	cp.Credentials = append([]webauthn.Credential{}, u.Credentials...)
	return &cp, nil
}

// Put implements UserStore.
func (s *MemUserStore) Put(user *CredentialUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *user
	cp.Credentials = append([]webauthn.Credential{}, user.Credentials...)
	s.users[user.Principal] = &cp
	return nil
}

// Service wraps go-webauthn's registration/assertion ceremonies with
// single-use challenge sessions: BeginRegistration/BeginLogin hand out a
// sessionID for a challenge that FinishRegistration/FinishLogin consume
// exactly once (popSession deletes it on first read). Presenting the same
// sessionID a second time — which is what replaying a captured assertion
// response looks like — fails with "session not found", independent of
// go-webauthn's own signature/counter checks. This is the replay defense
// docs/PLAN.md Task 25's threat test exercises.
type Service struct {
	wa    *webauthn.WebAuthn
	users UserStore

	mu       sync.Mutex
	sessions map[string]webauthn.SessionData
}

// NewService builds a Service from a go-webauthn Config and a UserStore.
func NewService(cfg *webauthn.Config, users UserStore) (*Service, error) {
	wa, err := webauthn.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("authn: init webauthn: %w", err)
	}
	return &Service{wa: wa, users: users, sessions: make(map[string]webauthn.SessionData)}, nil
}

// BeginRegistration starts a registration ceremony for principal (creating
// a fresh, credential-less user if none exists yet) and returns the
// credential creation options JSON a browser's navigator.credentials.create
// expects, plus an opaque, single-use sessionID.
func (s *Service) BeginRegistration(principal string) (optionsJSON []byte, sessionID string, err error) {
	user, err := s.getOrNewUser(principal)
	if err != nil {
		return nil, "", err
	}
	options, session, err := s.wa.BeginRegistration(user)
	if err != nil {
		return nil, "", fmt.Errorf("authn: begin webauthn registration: %w", err)
	}
	optionsJSON, err = json.Marshal(options)
	if err != nil {
		return nil, "", fmt.Errorf("authn: marshal registration options: %w", err)
	}
	sessionID, err = s.putSession(*session)
	if err != nil {
		return nil, "", err
	}
	return optionsJSON, sessionID, nil
}

// FinishRegistration consumes sessionID (single-use) and body (the
// browser's raw attestation response), verifies the ceremony via
// go-webauthn, and persists the new credential against principal.
func (s *Service) FinishRegistration(principal, sessionID string, body io.Reader) (*webauthn.Credential, error) {
	session, ok := s.popSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("authn: registration session %s not found or already used", sessionID)
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("authn: parse registration response: %w", err)
	}
	user, err := s.getOrNewUser(principal)
	if err != nil {
		return nil, err
	}
	cred, err := s.wa.CreateCredential(user, session, parsed)
	if err != nil {
		return nil, fmt.Errorf("authn: create webauthn credential: %w", err)
	}
	user.Credentials = append(user.Credentials, *cred)
	if err := s.users.Put(user); err != nil {
		return nil, fmt.Errorf("authn: persist webauthn credential: %w", err)
	}
	return cred, nil
}

// BeginLogin starts an assertion ceremony for principal, who must already
// have at least one registered credential.
func (s *Service) BeginLogin(principal string) (optionsJSON []byte, sessionID string, err error) {
	user, err := s.users.Get(principal)
	if err != nil {
		return nil, "", err
	}
	if len(user.Credentials) == 0 {
		return nil, "", fmt.Errorf("authn: principal %s has no registered webauthn credential", principal)
	}
	options, session, err := s.wa.BeginLogin(user)
	if err != nil {
		return nil, "", fmt.Errorf("authn: begin webauthn login: %w", err)
	}
	optionsJSON, err = json.Marshal(options)
	if err != nil {
		return nil, "", fmt.Errorf("authn: marshal login options: %w", err)
	}
	sessionID, err = s.putSession(*session)
	if err != nil {
		return nil, "", err
	}
	return optionsJSON, sessionID, nil
}

// Assertion is a verified WebAuthn login ceremony result: the raw bytes
// exactly as submitted (so callers can bind an approval record to
// AssertionHash, per docs/PLAN.md Task 25 Step 3) and the credential that
// signed it.
type Assertion struct {
	RawResponse   []byte
	AssertionHash string // sha256 hex digest of RawResponse
	Credential    *webauthn.Credential
}

// FinishLogin consumes sessionID (single-use) and body (the browser's raw
// assertion response), verifies the ceremony via go-webauthn — including
// its signature-counter clone-detection check — and persists the
// credential's updated counter so a later replay against a fresh session
// is still caught there too.
func (s *Service) FinishLogin(principal, sessionID string, body io.Reader) (*Assertion, error) {
	session, ok := s.popSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("authn: login session %s not found or already used", sessionID)
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("authn: read assertion body: %w", err)
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("authn: parse assertion response: %w", err)
	}
	user, err := s.users.Get(principal)
	if err != nil {
		return nil, err
	}
	cred, err := s.wa.ValidateLogin(user, session, parsed)
	if err != nil {
		return nil, fmt.Errorf("authn: validate webauthn assertion: %w", err)
	}
	user.Credentials = replaceCredential(user.Credentials, *cred)
	if err := s.users.Put(user); err != nil {
		return nil, fmt.Errorf("authn: persist updated webauthn credential: %w", err)
	}
	sum := sha256.Sum256(raw)
	return &Assertion{RawResponse: raw, AssertionHash: hex.EncodeToString(sum[:]), Credential: cred}, nil
}

func (s *Service) getOrNewUser(principal string) (*CredentialUser, error) {
	user, err := s.users.Get(principal)
	if errors.Is(err, ErrUserNotFound) {
		return &CredentialUser{Principal: principal}, nil
	}
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) putSession(session webauthn.SessionData) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()
	return id, nil
}

func (s *Service) popSession(id string) (webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	return session, ok
}

func replaceCredential(creds []webauthn.Credential, updated webauthn.Credential) []webauthn.Credential {
	out := make([]webauthn.Credential, len(creds))
	copy(out, creds)
	for i := range out {
		if bytes.Equal(out[i].ID, updated.ID) {
			out[i] = updated
			return out
		}
	}
	return append(out, updated)
}

func newSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("authn: generate session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
