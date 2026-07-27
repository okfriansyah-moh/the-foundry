package authn_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
)

func TestIssueAndVerifySession_RoundTrip(t *testing.T) {
	priv, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	token, err := authn.IssueSession(priv, "alice")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	principal, err := authn.VerifySession(&priv.PublicKey, token)
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if principal != "alice" {
		t.Fatalf("principal = %q, want alice", principal)
	}
}

func TestIssueSession_RejectsEmptyPrincipal(t *testing.T) {
	priv, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	if _, err := authn.IssueSession(priv, ""); err == nil {
		t.Fatal("expected empty principal to be rejected")
	}
}

// TestVerifySession_RejectsExpired is this task's threat test: "an expired
// session JWT must be rejected" (docs/PLAN.md Task 25 Step 5).
func TestVerifySession_RejectsExpired(t *testing.T) {
	priv, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	now := time.Now()
	tok, err := jwt.NewBuilder().
		Subject("alice").
		IssuedAt(now.Add(-time.Hour)).
		Expiration(now.Add(-time.Minute)). // expired one minute ago
		Build()
	if err != nil {
		t.Fatalf("build expired claims: %v", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.ES256(), priv))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	if _, err := authn.VerifySession(&priv.PublicKey, signed); err == nil {
		t.Fatal("expected expired session token to be rejected")
	}
}

func TestVerifySession_RejectsWrongKey(t *testing.T) {
	priv, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	attacker, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	token, err := authn.IssueSession(attacker, "alice")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if _, err := authn.VerifySession(&priv.PublicKey, token); err == nil {
		t.Fatal("expected a token signed by a different key to be rejected")
	}
}

// TestVerifySession_RejectsAlgNone guards against the classic JWT
// algorithm-confusion attack: a forged token asserting "alg":"none" (no
// signature required) must never be accepted just because it carries a
// plausible-looking payload.
func TestVerifySession_RejectsAlgNone(t *testing.T) {
	priv, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"sub":"attacker","exp":%d,"iat":%d}`,
		time.Now().Add(time.Hour).Unix(), time.Now().Unix(),
	)))
	forged := []byte(header + "." + payload + ".")

	if _, err := authn.VerifySession(&priv.PublicKey, forged); err == nil {
		t.Fatal("expected alg:none forged token to be rejected")
	}
}

// TestVerifySession_RejectsMismatchedAlg guards against presenting a
// validly-signed token under a different algorithm than this package
// pins (ES256) — a well-known algorithm-confusion class of attack.
// VerifySession must reject it purely on the alg mismatch, never fall
// back to trying other algorithms.
func TestVerifySession_RejectsMismatchedAlg(t *testing.T) {
	priv, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	ec384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384 key: %v", err)
	}
	tok, err := jwt.NewBuilder().Subject("alice").Expiration(time.Now().Add(time.Hour)).Build()
	if err != nil {
		t.Fatalf("build claims: %v", err)
	}
	// A syntactically valid, correctly-signed ES384 token -- just not
	// the ES256 this package pins.
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.ES384(), ec384))
	if err != nil {
		t.Fatalf("sign with ES384: %v", err)
	}
	if _, err := authn.VerifySession(&priv.PublicKey, signed); err == nil {
		t.Fatal("expected an ES384 token to be rejected by an ES256-pinned verifier")
	}
}
