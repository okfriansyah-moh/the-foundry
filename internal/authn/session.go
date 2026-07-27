package authn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// SessionTTL is the fixed lifetime of a session JWT issued by IssueSession
// (docs/PLAN.md Task 25 / Constitution C12: "session JWT (short-lived)").
// It is a package constant, not a configurable field, so a stolen token
// has a narrow, unextendable window rather than becoming a de facto
// long-lived bearer credential.
const SessionTTL = 15 * time.Minute

// sessionAlg is the only signing algorithm IssueSession/VerifySession ever
// produce or accept: ECDSA P-256 (asymmetric, per this task's
// implementation standard). Pinning it explicitly on both sign and verify
// is what makes VerifySession reject algorithm-confusion attempts
// (a token claiming "alg":"none", or any algorithm other than ES256)
// before any claim is trusted.
var sessionAlg = jwa.ES256()

// GenerateSessionKey creates a new ECDSA P-256 signing key pair for
// session JWTs. In production this would be loaded from configured secret
// storage rather than generated per process; it exists here for callers
// (tests, a locally-run login flow) that need a self-contained key.
func GenerateSessionKey() (*ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("authn: generate session signing key: %w", err)
	}
	return priv, nil
}

// IssueSession mints a short-lived session JWT bound to principal (the
// "sub" claim), signed under priv with sessionAlg.
func IssueSession(priv *ecdsa.PrivateKey, principal string) ([]byte, error) {
	if principal == "" {
		return nil, fmt.Errorf("authn: issue session: principal is required")
	}
	now := time.Now()
	tok, err := jwt.NewBuilder().
		Subject(principal).
		IssuedAt(now).
		NotBefore(now).
		Expiration(now.Add(SessionTTL)).
		Build()
	if err != nil {
		return nil, fmt.Errorf("authn: build session claims: %w", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(sessionAlg, priv))
	if err != nil {
		return nil, fmt.Errorf("authn: sign session token: %w", err)
	}
	return signed, nil
}

// VerifySession parses and verifies a session JWT, and returns the bound
// principal (the "sub" claim) on success. It enforces:
//
//   - the signature is valid under pub, using exactly sessionAlg —
//     jwt.WithKey pins the expected algorithm, so a token asserting
//     "alg":"none" (skip verification entirely) or any algorithm other
//     than ES256 (the classic algorithm-confusion attack) is rejected
//     here, before any claim is read, never as a fallback-allow;
//   - exp/nbf are honored — jwt.Parse validates these by default, so an
//     expired or not-yet-valid session is rejected here, not left to a
//     caller that might forget to check a timestamp.
func VerifySession(pub *ecdsa.PublicKey, token []byte) (string, error) {
	tok, err := jwt.Parse(token, jwt.WithKey(sessionAlg, pub))
	if err != nil {
		return "", fmt.Errorf("authn: verify session: %w", err)
	}
	principal, ok := tok.Subject()
	if !ok || principal == "" {
		return "", fmt.Errorf("authn: session token has no subject")
	}
	return principal, nil
}
