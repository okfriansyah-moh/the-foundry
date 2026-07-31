package api

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

func TestParseTier(t *testing.T) {
	cases := map[string]admission.Tier{"A0": admission.TierA0, "A1": admission.TierA1, "A2": admission.TierA2, "H": admission.TierH}
	for label, want := range cases {
		got, err := parseTier(label)
		if err != nil {
			t.Fatalf("parseTier(%q): %v", label, err)
		}
		if got != want {
			t.Errorf("parseTier(%q) = %v, want %v", label, got, want)
		}
	}
	if _, err := parseTier("bogus"); err == nil {
		t.Error("parseTier(\"bogus\") = nil error, want error")
	}
}

// TestResolvePlanContext_LoadsRealApprovedPlan proves the resolver Task
// 36 supplies to authn.ApproveHandler reads the plan's real,
// already-classified tier from the provenance store rather than trusting
// any client input (internal/authn.PlanContextResolver's own contract).
func TestResolvePlanContext_LoadsRealApprovedPlan(t *testing.T) {
	f := newTestFixture(t)

	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     "plan-h",
		PlanDigest: "sha256:plan-h",
		RiskTier:   admission.TierH,
		ApprovedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if err := provenance.Sign(f.signingKey.Private, ap); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := f.provStore.Insert(context.Background(), ap); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	planCtx, err := f.server.resolvePlanContext(context.Background(), "plan-h")
	if err != nil {
		t.Fatalf("resolvePlanContext: %v", err)
	}
	if planCtx.Tier != admission.TierH {
		t.Errorf("Tier = %v, want TierH", planCtx.Tier)
	}
	if !planCtx.RequiresStrongAuth() {
		t.Error("RequiresStrongAuth() = false for a TierH plan, want true")
	}
}

func TestResolvePlanContext_UnknownPlanErrors(t *testing.T) {
	f := newTestFixture(t)
	if _, err := f.server.resolvePlanContext(context.Background(), "does-not-exist"); err == nil {
		t.Error("resolvePlanContext for unknown plan = nil error, want error")
	}
}

// TestHandleApprovePlan_WiredEndToEnd proves POST /v1/plans/{id}/approve
// is really mounted and really calls through to internal/authn's
// ApproveHandler (Task 25) — not that ApproveHandler is correct (that is
// exhaustively covered by internal/authn's own tests), only that this
// task's wiring reaches it: a low-tier personal plan needs no WebAuthn
// step-up and is recorded.
func TestHandleApprovePlan_WiredEndToEnd(t *testing.T) {
	f := newTestFixture(t)

	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     "plan-low",
		PlanDigest: "sha256:plan-low",
		RiskTier:   admission.TierA0,
		ApprovedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if err := provenance.Sign(f.signingKey.Private, ap); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := f.provStore.Insert(context.Background(), ap); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rec := doRequest(f, "POST", "/v1/plans/plan-low/approve", f.bearerToken(t), "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestResolvePlanContext_OrgProfileRequiresStrongAuthBelowH proves Task 118
// (SEC-04): an organization-profile plan requires WebAuthn step-up even at a
// non-H tier — the hardcoded profile.Personal that made the Organization half
// of RequiresStrongAuth unreachable is gone.
func TestResolvePlanContext_OrgProfileRequiresStrongAuthBelowH(t *testing.T) {
	f := newTestFixture(t)

	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:      "plan-org",
		PlanDigest:  "sha256:plan-org",
		RiskTier:    admission.TierA2, // deliberately below H
		ProfileKind: "organization",
		ApprovedAt:  time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if err := provenance.Sign(f.signingKey.Private, ap); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := f.provStore.Insert(context.Background(), ap); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	planCtx, err := f.server.resolvePlanContext(context.Background(), "plan-org")
	if err != nil {
		t.Fatalf("resolvePlanContext: %v", err)
	}
	if planCtx.Tier == admission.TierH {
		t.Fatal("test setup bug: tier must be below H to prove the org-profile path")
	}
	if !planCtx.RequiresStrongAuth() {
		t.Error("an organization-profile plan must require strong auth even below tier H")
	}
}
