package authn_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// approveTestFixture wires up everything ApproveHandler needs against
// in-memory fakes: a provenance.Store (MemRawStore), a WebAuthn Service
// with one registered credential for testPrincipal, and a mux that routes
// POST /v1/plans/{id}/approve exactly like a real deployment's router
// would (net/http.ServeMux's method+wildcard patterns, so r.PathValue("id")
// is populated the same way it would be in production).
type approveTestFixture struct {
	mux        *http.ServeMux
	store      *provenance.Store
	signingKey provenance.KeyPair
	sessionKey *ecdsa.PrivateKey
	webAuthn   *authn.Service
	rp         virtualwebauthn.RelyingParty
	authr      virtualwebauthn.Authenticator
	cred       virtualwebauthn.Credential
	planTiers  map[string]authn.PlanContext
}

func newApproveTestFixture(t *testing.T) *approveTestFixture {
	t.Helper()

	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)

	sessionKey, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}

	userStore := authn.NewMemUserStore()
	waSvc, err := authn.NewService(&webauthn.Config{
		RPID:          testRPID,
		RPDisplayName: "Example Corp",
		RPOrigins:     []string{testRPOrigin},
	}, userStore)
	if err != nil {
		t.Fatalf("authn.NewService: %v", err)
	}

	f := &approveTestFixture{
		store:      store,
		signingKey: *kp,
		sessionKey: sessionKey,
		webAuthn:   waSvc,
		rp:         virtualwebauthn.RelyingParty{Name: "Example Corp", ID: testRPID, Origin: testRPOrigin},
		authr:      virtualwebauthn.NewAuthenticator(),
		cred:       virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2),
		planTiers:  make(map[string]authn.PlanContext),
	}
	registerVirtualCredential(t, f.webAuthn, f.rp, &f.authr, f.cred, testPrincipal)

	handler := &authn.ApproveHandler{
		SessionPub: &sessionKey.PublicKey,
		WebAuthn:   waSvc,
		Store:      store,
		SigningKey: kp.Private,
		ResolveContext: func(_ context.Context, planID string) (authn.PlanContext, error) {
			ctx, ok := f.planTiers[planID]
			if !ok {
				return authn.PlanContext{}, fmt.Errorf("unknown plan %s", planID)
			}
			return ctx, nil
		},
	}
	f.mux = http.NewServeMux()
	f.mux.Handle("POST /v1/plans/{id}/approve", handler)
	return f
}

// addPlan signs and inserts a minimal ApprovedPlan under planID with the
// given tier, and registers its PlanContext for the fixture's resolver.
func (f *approveTestFixture) addPlan(t *testing.T, planID string, tier admission.Tier, kind profile.Kind) {
	t.Helper()
	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     planID,
		PlanDigest: "sha256:" + planID,
		RiskTier:   tier,
		ApprovedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if err := provenance.Sign(f.signingKey.Private, ap); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := f.store.Insert(context.Background(), ap); err != nil {
		t.Fatalf("Store.Insert: %v", err)
	}
	f.planTiers[planID] = authn.PlanContext{Tier: tier, Profile: kind}
}

func (f *approveTestFixture) validSessionToken(t *testing.T) string {
	t.Helper()
	tok, err := authn.IssueSession(f.sessionKey, testPrincipal)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	return string(tok)
}

// webauthnAssertionJSON runs a full login ceremony via virtualwebauthn
// against f.webAuthn and returns the sessionID plus the raw assertion
// response JSON, ready to embed in an approve request body.
func (f *approveTestFixture) webauthnAssertionJSON(t *testing.T) (sessionID, assertionJSON string) {
	t.Helper()
	optionsJSON, sessionID, err := f.webAuthn.BeginLogin(testPrincipal)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	assertionOptions, err := virtualwebauthn.ParseAssertionOptions(string(optionsJSON))
	if err != nil {
		t.Fatalf("ParseAssertionOptions: %v", err)
	}
	assertionJSON = virtualwebauthn.CreateAssertionResponse(f.rp, f.authr, f.cred, *assertionOptions)
	return sessionID, assertionJSON
}

func (f *approveTestFixture) do(t *testing.T, planID, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/plans/"+planID+"/approve", strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// TestApproveHandler_HighTierWithoutWebAuthn_Is403 is this task's
// Acceptance criterion: "H-tier approve without WebAuthn ⇒ 403."
func TestApproveHandler_HighTierWithoutWebAuthn_Is403(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-h", admission.TierH, profile.Personal)

	rec := f.do(t, "plan-h", f.validSessionToken(t), "{}")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

// TestApproveHandler_HighTierWithWebAuthn_Is200Recorded is this task's
// other Acceptance criterion: "with ⇒ recorded approver incl. method."
func TestApproveHandler_HighTierWithWebAuthn_Is200Recorded(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-h", admission.TierH, profile.Personal)

	sessionID, assertionJSON := f.webauthnAssertionJSON(t)
	body := fmt.Sprintf(`{"webauthn_session_id":%q,"webauthn_assertion":%s}`, sessionID, assertionJSON)

	rec := f.do(t, "plan-h", f.validSessionToken(t), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Approver struct {
			Principal     string `json:"principal"`
			Method        string `json:"method"`
			AssertionHash string `json:"assertion_hash"`
		} `json:"approver"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Approver.Principal != testPrincipal {
		t.Fatalf("approver.principal = %q, want %q", resp.Approver.Principal, testPrincipal)
	}
	if resp.Approver.Method != authn.AuthMethodOIDCWebAuthn {
		t.Fatalf("approver.method = %q, want %q", resp.Approver.Method, authn.AuthMethodOIDCWebAuthn)
	}
	if resp.Approver.AssertionHash == "" {
		t.Fatal("expected a non-empty assertion hash")
	}

	loaded, err := f.store.Load(context.Background(), "plan-h")
	if err != nil {
		t.Fatalf("Store.Load: %v", err)
	}
	approvers := loaded.Approvers()
	if len(approvers) != 1 || approvers[0].Method != authn.AuthMethodOIDCWebAuthn {
		t.Fatalf("persisted approvers = %+v, want one oidc+webauthn approver", approvers)
	}
}

// TestApproveHandler_OrganizationProfileLowTier_RequiresWebAuthn proves
// tier and profile are independently sufficient (an OR, not an AND):
// even at the lowest tier, an organization-profile plan still requires
// step-up.
func TestApproveHandler_OrganizationProfileLowTier_RequiresWebAuthn(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-org", admission.TierA0, profile.Organization)

	rec := f.do(t, "plan-org", f.validSessionToken(t), "{}")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

// TestApproveHandler_LowTierPersonal_NoStepUpRequired proves the
// endpoint doesn't demand WebAuthn when it isn't required.
func TestApproveHandler_LowTierPersonal_NoStepUpRequired(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-low", admission.TierA0, profile.Personal)

	rec := f.do(t, "plan-low", f.validSessionToken(t), "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// TestApproveHandler_RejectsMissingSession proves an unauthenticated
// request is rejected before any strong-auth logic runs at all.
func TestApproveHandler_RejectsMissingSession(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-low", admission.TierA0, profile.Personal)

	rec := f.do(t, "plan-low", "", "{}")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", rec.Code, rec.Body.String())
	}
}

// TestApproveHandler_RejectsReplayedWebAuthnAssertion is the approve
// endpoint's own version of this task's WebAuthn replay threat test:
// submitting the exact same (session, assertion) pair twice must not
// record two approvals, nor succeed the second time.
func TestApproveHandler_RejectsReplayedWebAuthnAssertion(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-h", admission.TierH, profile.Personal)

	sessionID, assertionJSON := f.webauthnAssertionJSON(t)
	body := fmt.Sprintf(`{"webauthn_session_id":%q,"webauthn_assertion":%s}`, sessionID, assertionJSON)
	token := f.validSessionToken(t)

	first := f.do(t, "plan-h", token, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first approval status = %d, want 200; body: %s", first.Code, first.Body.String())
	}

	second := f.do(t, "plan-h", token, body)
	if second.Code != http.StatusForbidden {
		t.Fatalf("replayed approval status = %d, want 403; body: %s", second.Code, second.Body.String())
	}
}

// TestApproveHandler_RejectsApprovalOnRevokedPlan closes the
// secondary-review finding on Task 25 (docs/PLAN.md Task 25 Status line,
// "Secondary AI-agent review"): even a fully strong-auth-verified request
// must not succeed against a plan that was revoked out from under it —
// ApproveHandler must surface provenance.Store.AddApprover's rejection as
// an HTTP error, not a 200, and must not leak the internal error text.
func TestApproveHandler_RejectsApprovalOnRevokedPlan(t *testing.T) {
	f := newApproveTestFixture(t)
	f.addPlan(t, "plan-low", admission.TierA0, profile.Personal)

	if _, err := f.store.Revoke(context.Background(), "plan-low", f.signingKey.Private, "security-team", "compromised credential"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	rec := f.do(t, "plan-low", f.validSessionToken(t), "{}")
	if rec.Code == http.StatusOK {
		t.Fatalf("expected approval on a revoked plan to be rejected, got 200; body: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "revoked") {
		t.Fatalf("response leaked internal error detail: %s", rec.Body.String())
	}
}
