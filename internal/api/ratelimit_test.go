package api

import (
	"crypto/ecdsa"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// buildRateLimitedServer wires a Server the same way newTestFixture does,
// except it also sets Dependencies.RateLimiter/IntakeQueue — the two
// fields docs/PLAN.md Task 95 adds — so this file's tests can drive
// observe.Middleware's real behavior end-to-end through Server.ServeHTTP,
// keyed by this server's real rateLimitKeyFunc, rather than re-testing
// observe.Middleware in isolation (already covered by
// internal/observe/limits_test.go).
func buildRateLimitedServer(t *testing.T, rl *observe.Limiter, iq *observe.IntakeQueue) (*Server, *ecdsa.PrivateKey) {
	t.Helper()

	db, err := sql.Open("pgx", "postgres://fake:fake@127.0.0.1:1/fake?sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	provStore := provenance.NewStore(provenance.NewMemRawStore(), kp.Public)

	sessionKey, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}

	waSvc, err := authn.NewService(&webauthn.Config{
		RPID:          testRPID,
		RPDisplayName: "Test",
		RPOrigins:     []string{testRPOrigin},
	}, authn.NewMemUserStore())
	if err != nil {
		t.Fatalf("authn.NewService: %v", err)
	}

	profiles := profile.NewStore(profile.NewMemRawStore())
	evStore := evidence.NewFSStore(t.TempDir())
	decider := &allowAllDecider{}

	srv, err := NewServer(Dependencies{
		DB:                 db,
		TemporalHostPort:   "127.0.0.1:1",
		TemporalNamespace:  "default",
		Evidence:           evStore,
		Profiles:           profiles,
		Provenance:         provStore,
		ApprovalSigningKey: kp.Private,
		SessionPub:         &sessionKey.PublicKey,
		WebAuthn:           waSvc,
		Decider:            decider,
		PolicyDigest:       "sha256:test-digest",
		RateLimiter:        rl,
		IntakeQueue:        iq,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	return srv, sessionKey
}

func bearerFor(t *testing.T, sessionKey *ecdsa.PrivateKey, principal string) string {
	t.Helper()
	tok, err := authn.IssueSession(sessionKey, principal)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	return string(tok)
}

// requestFrom builds a GET /v1/profiles request (an authorize()-wrapped,
// no-body route) carrying bearer as its session token (empty = no
// Authorization header, i.e. anonymous/unauthenticated) and remoteAddr as
// its client IP.
func requestFrom(bearer, remoteAddr string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/profiles", nil)
	r.RemoteAddr = remoteAddr
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	return r
}

// TestServeHTTP_AuthenticatedPrincipalsGetIndependentBuckets proves the
// keyFunc Task 95 wires (observe.PrincipalOrIPWithAuth over
// principalFromRequest) keys distinct authenticated sessions
// independently: exhausting alice's bucket must not throttle bob, even
// though both requests share a rate limiter and the same client IP.
func TestServeHTTP_AuthenticatedPrincipalsGetIndependentBuckets(t *testing.T) {
	rl := observe.NewLimiter(0, 1) // burst=1, no refill: a 2nd request for the same key always 429s.
	srv, sessionKey := buildRateLimitedServer(t, rl, nil)

	aliceTok := bearerFor(t, sessionKey, "alice@example.com")
	bobTok := bearerFor(t, sessionKey, "bob@example.com")
	const sharedIP = "203.0.113.20:5555"

	rec1 := httptest.NewRecorder()
	srv.ServeHTTP(rec1, requestFrom(aliceTok, sharedIP))
	if rec1.Code != http.StatusOK {
		t.Fatalf("alice's 1st request = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, requestFrom(aliceTok, sharedIP))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("alice's 2nd request = %d, want 429 (bucket exhausted)", rec2.Code)
	}

	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, requestFrom(bobTok, sharedIP))
	if rec3.Code != http.StatusOK {
		t.Fatalf("bob's 1st request = %d, want 200 (independent bucket from alice's)", rec3.Code)
	}
}

// TestServeHTTP_UnauthenticatedFallsBackToIP proves an unauthenticated
// request (no/invalid bearer token — principalFromRequest fails) is keyed
// by client IP via observe.PrincipalOrIP's fallback, not left unkeyed or
// erroneously authenticated.
func TestServeHTTP_UnauthenticatedFallsBackToIP(t *testing.T) {
	rl := observe.NewLimiter(0, 1)
	srv, _ := buildRateLimitedServer(t, rl, nil)

	const ip1 = "198.51.100.1:1"
	const ip2 = "198.51.100.2:1"

	rec1 := httptest.NewRecorder()
	srv.ServeHTTP(rec1, requestFrom("", ip1))
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("1st unauthenticated request = %d, want 401 (no session, but not rate-limited away)", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, requestFrom("", ip1))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd unauthenticated request from same IP = %d, want 429 (IP-keyed bucket exhausted)", rec2.Code)
	}

	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, requestFrom("", ip2))
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("1st unauthenticated request from a different IP = %d, want 401 (independent bucket)", rec3.Code)
	}
}

// TestServeHTTP_IntakeQueueFullRejectsRegardlessOfPrincipal proves the
// bounded-admission check runs regardless of who the caller is: a full
// IntakeQueue rejects even an authenticated principal that has never
// touched the rate limiter.
func TestServeHTTP_IntakeQueueFullRejectsRegardlessOfPrincipal(t *testing.T) {
	iq := observe.NewIntakeQueue("test-api-intake", 1)
	if err := iq.TryEnqueue(); err != nil {
		t.Fatalf("occupy the only slot: %v", err)
	}
	t.Cleanup(iq.Release)

	srv, sessionKey := buildRateLimitedServer(t, nil, iq)
	tok := bearerFor(t, sessionKey, "alice@example.com")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, requestFrom(tok, "203.0.113.30:1"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request against a full intake queue = %d, want 429", rec.Code)
	}
}
