// Package oidc provides a minimal, local OIDC-compliant test IdP
// (docs/PLAN.md Task 25 Outputs: test/fakes/oidc). It implements just
// enough of OIDC Discovery, JWKS, and RFC 8628 (Device Authorization
// Grant) for internal/authn.StartDeviceLogin/FinishDeviceLogin to run an
// end-to-end device-code login against it in tests, without depending on
// a live managed IdP (docs/PLAN.md Blocker B5: managed IdP, Zitadel-class
// OIDC, is the production default — this fake exists purely so Task 25's
// OIDC code path isn't skipped for lack of one).
//
// It is deliberately not a general-purpose OIDC conformance fake: every
// device code is authorized the instant it's issued (no simulated human
// browser click), and there is exactly one fixed test subject. ID tokens
// are signed with a real RSA key via github.com/lestrrat-go/jwx/v3 — no
// hand-rolled JWT signing, matching this task's own Boundary.
package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Subject is the fixed principal every device login through this fake
// authenticates as.
const Subject = "test-user@example.com"

const keyID = "fake-oidc-test-key"

// Server is a fake OIDC IdP backed by an httptest.Server.
type Server struct {
	*httptest.Server

	// ClientID is the only client_id this fake accepts.
	ClientID string

	privJWK jwk.Key
	jwks    jwk.Set

	mu      sync.Mutex
	devices map[string]struct{} // known device_codes; presence == authorized
}

// NewServer starts a fake IdP. Callers must defer Close().
func NewServer() (*Server, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("fakes/oidc: generate signing key: %w", err)
	}

	privJWK, err := jwk.Import(key)
	if err != nil {
		return nil, fmt.Errorf("fakes/oidc: import signing key: %w", err)
	}
	if err := privJWK.Set(jwk.KeyIDKey, keyID); err != nil {
		return nil, fmt.Errorf("fakes/oidc: set kid: %w", err)
	}

	pubJWK, err := jwk.PublicKeyOf(privJWK)
	if err != nil {
		return nil, fmt.Errorf("fakes/oidc: derive public key: %w", err)
	}
	if err := pubJWK.Set(jwk.AlgorithmKey, jwa.RS256()); err != nil {
		return nil, fmt.Errorf("fakes/oidc: set alg: %w", err)
	}
	if err := pubJWK.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return nil, fmt.Errorf("fakes/oidc: set use: %w", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(pubJWK); err != nil {
		return nil, fmt.Errorf("fakes/oidc: build jwks: %w", err)
	}

	s := &Server{
		ClientID: "foundry-cli-test",
		privJWK:  privJWK,
		jwks:     set,
		devices:  make(map[string]struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.handleDiscovery)
	mux.HandleFunc("/jwks", s.handleJWKS)
	mux.HandleFunc("/device_authorization", s.handleDeviceAuthorization)
	mux.HandleFunc("/token", s.handleToken)
	s.Server = httptest.NewServer(mux)
	return s, nil
}

func (s *Server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                s.URL,
		"authorization_endpoint":                s.URL + "/authorize",
		"token_endpoint":                        s.URL + "/token",
		"device_authorization_endpoint":         s.URL + "/device_authorization",
		"jwks_uri":                              s.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	buf, err := json.Marshal(s.jwks)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(buf)
}

func (s *Server) handleDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("client_id") != s.ClientID {
		writeOAuthError(w, http.StatusUnauthorized, "unauthorized_client")
		return
	}

	deviceCode := randomToken()
	s.mu.Lock()
	s.devices[deviceCode] = struct{}{} // authorized immediately -- see package doc
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 randomToken()[:8],
		"verification_uri":          s.URL + "/device",
		"verification_uri_complete": s.URL + "/device?user_code=stub",
		"expires_in":                600,
		"interval":                  1,
	})
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if r.PostForm.Get("client_id") != s.ClientID {
		writeOAuthError(w, http.StatusUnauthorized, "unauthorized_client")
		return
	}
	deviceCode := r.PostForm.Get("device_code")
	s.mu.Lock()
	_, known := s.devices[deviceCode]
	s.mu.Unlock()
	if !known {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	idToken, err := s.issueIDToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": randomToken(),
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func (s *Server) issueIDToken() (string, error) {
	now := time.Now()
	tok, err := jwt.NewBuilder().
		Issuer(s.URL).
		Subject(Subject).
		Audience([]string{s.ClientID}).
		IssuedAt(now).
		Expiration(now.Add(5 * time.Minute)).
		Build()
	if err != nil {
		return "", fmt.Errorf("fakes/oidc: build id_token claims: %w", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), s.privJWK))
	if err != nil {
		return "", fmt.Errorf("fakes/oidc: sign id_token: %w", err)
	}
	return string(signed), nil
}

func randomToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%x", buf)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
