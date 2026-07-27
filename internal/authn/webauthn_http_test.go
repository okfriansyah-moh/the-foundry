package authn_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
)

func newWebAuthnHTTPFixture(t *testing.T) (*http.ServeMux, *authn.Service, func(principal string) string) {
	t.Helper()
	svc, err := authn.NewService(&webauthn.Config{
		RPID:          testRPID,
		RPDisplayName: "Example Corp",
		RPOrigins:     []string{testRPOrigin},
	}, authn.NewMemUserStore())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	sessionKey, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	h := &authn.WebAuthnHTTP{SessionPub: &sessionKey.PublicKey, Service: svc}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/webauthn/register/begin", h.BeginRegistration)
	mux.HandleFunc("POST /v1/webauthn/register/finish", h.FinishRegistration)
	mux.HandleFunc("POST /v1/webauthn/login/begin", h.BeginLogin)

	tokenFor := func(principal string) string {
		tok, err := authn.IssueSession(sessionKey, principal)
		if err != nil {
			t.Fatalf("IssueSession: %v", err)
		}
		return string(tok)
	}
	return mux, svc, tokenFor
}

func TestWebAuthnHTTP_RegisterAndLoginBegin_EndToEnd(t *testing.T) {
	mux, _, tokenFor := newWebAuthnHTTPFixture(t)
	token := tokenFor(testPrincipal)

	rp := virtualwebauthn.RelyingParty{Name: "Example Corp", ID: testRPID, Origin: testRPOrigin}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	// Begin registration.
	beginReq := httptest.NewRequest(http.MethodPost, "/v1/webauthn/register/begin", nil)
	beginReq.Header.Set("Authorization", "Bearer "+token)
	beginRec := httptest.NewRecorder()
	mux.ServeHTTP(beginRec, beginReq)
	if beginRec.Code != http.StatusOK {
		t.Fatalf("register/begin status = %d, body: %s", beginRec.Code, beginRec.Body.String())
	}
	var begin struct {
		SessionID string          `json:"session_id"`
		Options   json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(beginRec.Body.Bytes(), &begin); err != nil {
		t.Fatalf("decode begin response: %v", err)
	}

	attestationOptions, err := virtualwebauthn.ParseAttestationOptions(string(begin.Options))
	if err != nil {
		t.Fatalf("ParseAttestationOptions: %v", err)
	}
	attestationResponse := virtualwebauthn.CreateAttestationResponse(rp, authr, cred, *attestationOptions)

	// Finish registration.
	finishReq := httptest.NewRequest(http.MethodPost, "/v1/webauthn/register/finish?session_id="+begin.SessionID, strings.NewReader(attestationResponse))
	finishReq.Header.Set("Authorization", "Bearer "+token)
	finishRec := httptest.NewRecorder()
	mux.ServeHTTP(finishRec, finishReq)
	if finishRec.Code != http.StatusOK {
		t.Fatalf("register/finish status = %d, body: %s", finishRec.Code, finishRec.Body.String())
	}
	authr.AddCredential(cred)

	// Begin login now that a credential is registered.
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/webauthn/login/begin", nil)
	loginReq.Header.Set("Authorization", "Bearer "+token)
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login/begin status = %d, body: %s", loginRec.Code, loginRec.Body.String())
	}
	var loginBegin struct {
		SessionID string          `json:"session_id"`
		Options   json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginBegin); err != nil {
		t.Fatalf("decode login/begin response: %v", err)
	}
	if loginBegin.SessionID == "" {
		t.Fatal("expected a non-empty login session id")
	}
}

func TestWebAuthnHTTP_RejectsMissingSession(t *testing.T) {
	mux, _, _ := newWebAuthnHTTPFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/webauthn/register/begin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestWebAuthnHTTP_LoginBegin_RejectsPrincipalWithNoCredential(t *testing.T) {
	mux, _, tokenFor := newWebAuthnHTTPFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/webauthn/login/begin", nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor("nobody@example.com"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
