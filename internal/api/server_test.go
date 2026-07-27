package api

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/policy"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

const (
	testRPID      = "localhost"
	testRPOrigin  = "http://localhost"
	testPrincipal = "alice@example.com"
)

// allowAllDecider is a fake policy.Decider that grants every request whose
// Principal is non-empty and denies otherwise — enough to unit-test this
// package's own routing/handler logic without depending on
// internal/policy/pdp's real OPA bundle (that integration is covered by
// TestServer_UsesRealOPADecider, which builds one for real).
type allowAllDecider struct {
	lastRequest policy.Request
	deny        bool
}

func (d *allowAllDecider) Decide(_ context.Context, req policy.Request) (policy.Decision, error) {
	d.lastRequest = req
	if d.deny || req.Principal == "" {
		return policy.Decision{Allow: false, Reason: "denied: test decider"}, nil
	}
	return policy.Decision{Allow: true, Reason: "allowed"}, nil
}

// testFixture wires a Server against in-memory/fake collaborators: no
// Docker/Postgres/Temporal is required to exercise routing, auth, and
// every handler that doesn't itself need a live database (submit,
// approve, evidence, profiles). sql.Open is lazy (no dial until a query
// runs), so DB can be a non-nil *sql.DB that is never actually reached by
// these tests.
type testFixture struct {
	server     *Server
	decider    *allowAllDecider
	sessionKey *ecdsa.PrivateKey
	provStore  *provenance.Store
	signingKey provenance.KeyPair
	profiles   *profile.Store
	evidence   *evidence.FSStore
}

func newTestFixture(t *testing.T) *testFixture {
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
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	return &testFixture{
		server:     srv,
		decider:    decider,
		sessionKey: sessionKey,
		provStore:  provStore,
		signingKey: *kp,
		profiles:   profiles,
		evidence:   evStore,
	}
}

func (f *testFixture) bearerToken(t *testing.T) string {
	t.Helper()
	tok, err := authn.IssueSession(f.sessionKey, testPrincipal)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	return string(tok)
}
