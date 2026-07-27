package provenance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// buildApprovedExpiringAt builds and signs an ApprovedPlan whose
// ExpiresAt is exactly expiresAt — used to construct already-expired
// fixtures, which buildApprovedAndSign (fixed at now+24h) cannot.
func buildApprovedExpiringAt(t *testing.T, doc *plan.Document, allow provenance.AllowList, expiresAt time.Time) (*provenance.ApprovedPlan, *provenance.KeyPair) {
	t.Helper()
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	decision, err := admission.Classify(doc, admission.NoopPolicyView{})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	now := time.Now().UTC()
	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:              doc.ID,
		PlanDigest:          doc.DigestHex(),
		CreatorPrincipal:    "alice",
		SubmittingPrincipal: "alice",
		ClassifierVersion:   decision.ClassifierVersion,
		Declared:            decision.Declared,
		Requested:           doc.RequestedPermissions,
		Scope:               provenance.Scope{Repositories: []string{"https://github.com/example/fixture"}},
		RiskTier:            decision.Tier,
		BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: doc.BudgetUSD, WorkflowUSD: doc.BudgetUSD},
		DataClass:           "internal",
		Approvers:           []provenance.Approver{{Principal: "alice", Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:          now,
		ExpiresAt:           expiresAt,
	}, allow)
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}
	if err := provenance.Sign(kp.Private, ap); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return ap, kp
}

// TestStore_Load_RejectsExpiredPlan is Task 24's "enforce expires_at on
// Load" acceptance case: a signed, otherwise-valid ApprovedPlan whose
// ExpiresAt has already passed must be rejected by Store.Load, not merely
// carry an unenforced flag (Constitution C7 rule 5).
func TestStore_Load_RejectsExpiredPlan(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedExpiringAt(t, doc, mustLoadAllowList(t), time.Now().Add(-time.Hour))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err := store.Load(ctx, ap.PlanID())
	if err == nil {
		t.Fatal("expected Load to reject an expired ApprovedPlan")
	}
	if !errors.Is(err, provenance.ErrPlanExpired) {
		t.Fatalf("expected errors.Is(err, ErrPlanExpired), got %v", err)
	}
}

// TestStore_Load_RejectsPlanAfterRevocation is Task 24's revocation-enforcement
// acceptance case: once Store.Revoke has run, the very next Load must
// reject it — no caching window between revocation and enforcement.
func TestStore_Load_RejectsPlanAfterRevocation(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Sanity: unrevoked plan loads fine.
	if _, err := store.Load(ctx, ap.PlanID()); err != nil {
		t.Fatalf("load before revocation: %v", err)
	}

	if _, err := store.Revoke(ctx, ap.PlanID(), kp.Private, "security-team", "compromised credential"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, err := store.Load(ctx, ap.PlanID())
	if err == nil {
		t.Fatal("expected Load to reject a revoked ApprovedPlan")
	}
	if !errors.Is(err, provenance.ErrPlanRevoked) {
		t.Fatalf("expected errors.Is(err, ErrPlanRevoked), got %v", err)
	}
}

// TestStore_Revocation_PersistsReasonAndReSigns confirms the revocation
// itself becomes part of the signed artifact (RevokedBy/RevocationReason
// round-trip through the RawStore, and the re-signed row still verifies)
// rather than a side-channel flag that could be stripped without
// invalidating the signature.
func TestStore_Revocation_PersistsReasonAndReSigns(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	revoked, err := store.Revoke(ctx, ap.PlanID(), kp.Private, "bob", "plan superseded")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !revoked.Revoked() {
		t.Fatal("expected Revoked() to be true after Revoke")
	}
	if revoked.RevokedBy() != "bob" {
		t.Fatalf("RevokedBy() = %q, want bob", revoked.RevokedBy())
	}
	if revoked.RevocationReason() != "plan superseded" {
		t.Fatalf("RevocationReason() = %q, want %q", revoked.RevocationReason(), "plan superseded")
	}

	// Verify the persisted row (read via the raw seam, bypassing Store.Load's
	// revocation gate) still passes signature verification and carries the
	// revocation fields.
	stored, err := raw.Load(ctx, ap.PlanID())
	if err != nil {
		t.Fatalf("raw load: %v", err)
	}
	if err := provenance.Verify(kp.Public, stored); err != nil {
		t.Fatalf("expected re-signed revoked plan to still verify: %v", err)
	}
	if !stored.Revoked() || stored.RevokedBy() != "bob" {
		t.Fatalf("persisted row missing revocation fields: %+v", stored)
	}
}

// TestStore_Revocation_WorksOnAlreadyExpiredPlan proves revocation is an
// administrative action independent of Store.Load's own expiry gate: an
// operator must be able to revoke a plan that has already expired.
func TestStore_Revocation_WorksOnAlreadyExpiredPlan(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedExpiringAt(t, doc, mustLoadAllowList(t), time.Now().Add(-time.Hour))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := store.Revoke(ctx, ap.PlanID(), kp.Private, "bob", "cleanup"); err != nil {
		t.Fatalf("expected Revoke to succeed on an already-expired plan, got: %v", err)
	}
}

// TestStore_Revocation_RequiresReasonAndActor confirms Revoke fails closed
// on a missing reason or actor rather than silently recording an empty
// audit trail.
func TestStore_Revocation_RequiresReasonAndActor(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := store.Revoke(ctx, ap.PlanID(), kp.Private, "bob", ""); err == nil {
		t.Fatal("expected Revoke to reject an empty reason")
	}
	if _, err := store.Revoke(ctx, ap.PlanID(), kp.Private, "", "some reason"); err == nil {
		t.Fatal("expected Revoke to reject an empty revokedBy")
	}
}
