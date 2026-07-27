package authn_test

import (
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
)

const (
	testRPID      = "example.com"
	testRPOrigin  = "https://example.com"
	testPrincipal = "alice@example.com"
)

func newTestWebAuthnService(t *testing.T) (*authn.Service, *authn.MemUserStore) {
	t.Helper()
	store := authn.NewMemUserStore()
	svc, err := authn.NewService(&webauthn.Config{
		RPID:          testRPID,
		RPDisplayName: "Example Corp",
		RPOrigins:     []string{testRPOrigin},
	}, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, store
}

// registerVirtualCredential runs a full registration ceremony against svc
// using a virtualwebauthn authenticator, so tests exercise the exact
// go-webauthn code path a real browser+authenticator would drive.
func registerVirtualCredential(t *testing.T, svc *authn.Service, rp virtualwebauthn.RelyingParty, authr *virtualwebauthn.Authenticator, cred virtualwebauthn.Credential, principal string) *webauthn.Credential {
	t.Helper()

	optionsJSON, sessionID, err := svc.BeginRegistration(principal)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	attestationOptions, err := virtualwebauthn.ParseAttestationOptions(string(optionsJSON))
	if err != nil {
		t.Fatalf("ParseAttestationOptions: %v", err)
	}
	attestationResponse := virtualwebauthn.CreateAttestationResponse(rp, *authr, cred, *attestationOptions)

	registered, err := svc.FinishRegistration(principal, sessionID, strings.NewReader(attestationResponse))
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	authr.AddCredential(cred)
	return registered
}

func TestWebAuthn_RegisterAndLogin(t *testing.T) {
	svc, _ := newTestWebAuthnService(t)
	rp := virtualwebauthn.RelyingParty{Name: "Example Corp", ID: testRPID, Origin: testRPOrigin}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	registerVirtualCredential(t, svc, rp, &authr, cred, testPrincipal)

	optionsJSON, sessionID, err := svc.BeginLogin(testPrincipal)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	assertionOptions, err := virtualwebauthn.ParseAssertionOptions(string(optionsJSON))
	if err != nil {
		t.Fatalf("ParseAssertionOptions: %v", err)
	}
	assertionResponse := virtualwebauthn.CreateAssertionResponse(rp, authr, cred, *assertionOptions)

	assertion, err := svc.FinishLogin(testPrincipal, sessionID, strings.NewReader(assertionResponse))
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if assertion.AssertionHash == "" {
		t.Fatal("expected a non-empty assertion hash")
	}
}

func TestWebAuthn_BeginLogin_RejectsUnknownPrincipal(t *testing.T) {
	svc, _ := newTestWebAuthnService(t)
	if _, _, err := svc.BeginLogin("nobody@example.com"); err == nil {
		t.Fatal("expected BeginLogin for a principal with no credentials to fail")
	}
}

// TestWebAuthn_RejectsReplayedAssertion is this task's threat test: "a
// replayed WebAuthn assertion (same assertion presented twice) must be
// rejected" (docs/PLAN.md Task 25 Step 5). The defense is that
// BeginLogin's session/challenge is single-use: FinishLogin consumes
// (pops) it, so presenting the very same assertion response bytes to
// FinishLogin a second time cannot possibly find that session again.
func TestWebAuthn_RejectsReplayedAssertion(t *testing.T) {
	svc, _ := newTestWebAuthnService(t)
	rp := virtualwebauthn.RelyingParty{Name: "Example Corp", ID: testRPID, Origin: testRPOrigin}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	registerVirtualCredential(t, svc, rp, &authr, cred, testPrincipal)

	optionsJSON, sessionID, err := svc.BeginLogin(testPrincipal)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	assertionOptions, err := virtualwebauthn.ParseAssertionOptions(string(optionsJSON))
	if err != nil {
		t.Fatalf("ParseAssertionOptions: %v", err)
	}
	assertionResponse := virtualwebauthn.CreateAssertionResponse(rp, authr, cred, *assertionOptions)

	if _, err := svc.FinishLogin(testPrincipal, sessionID, strings.NewReader(assertionResponse)); err != nil {
		t.Fatalf("first FinishLogin (legitimate) failed: %v", err)
	}

	// Replay: the identical (sessionID, assertionResponse) pair, resubmitted.
	if _, err := svc.FinishLogin(testPrincipal, sessionID, strings.NewReader(assertionResponse)); err == nil {
		t.Fatal("expected a replayed assertion to be rejected")
	}
}

// TestWebAuthn_RejectsAssertionAgainstFreshSession proves the replay
// defense isn't merely "you can't reuse a sessionID string" — the
// signed clientDataJSON itself is bound to the challenge from the
// session it was created for, so replaying old response bytes against a
// brand new BeginLogin session (a fresh challenge) is rejected by
// go-webauthn's own challenge check too, not just this package's
// single-use bookkeeping.
func TestWebAuthn_RejectsAssertionAgainstFreshSession(t *testing.T) {
	svc, _ := newTestWebAuthnService(t)
	rp := virtualwebauthn.RelyingParty{Name: "Example Corp", ID: testRPID, Origin: testRPOrigin}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	registerVirtualCredential(t, svc, rp, &authr, cred, testPrincipal)

	optionsJSON1, sessionID1, err := svc.BeginLogin(testPrincipal)
	if err != nil {
		t.Fatalf("BeginLogin (1): %v", err)
	}
	assertionOptions1, err := virtualwebauthn.ParseAssertionOptions(string(optionsJSON1))
	if err != nil {
		t.Fatalf("ParseAssertionOptions (1): %v", err)
	}
	oldResponse := virtualwebauthn.CreateAssertionResponse(rp, authr, cred, *assertionOptions1)
	// Consume session 1 so its challenge is gone from the map, forcing the
	// second attempt below to be checked purely on session 2's own state.
	if _, err := svc.FinishLogin(testPrincipal, sessionID1, strings.NewReader(oldResponse)); err != nil {
		t.Fatalf("first FinishLogin (legitimate) failed: %v", err)
	}

	_, sessionID2, err := svc.BeginLogin(testPrincipal)
	if err != nil {
		t.Fatalf("BeginLogin (2): %v", err)
	}
	// oldResponse's clientDataJSON carries session 1's challenge, not
	// session 2's -- go-webauthn must reject the mismatch.
	if _, err := svc.FinishLogin(testPrincipal, sessionID2, strings.NewReader(oldResponse)); err == nil {
		t.Fatal("expected an old assertion response to be rejected against a fresh session's challenge")
	}
}
