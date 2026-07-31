package provenance_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// TestApprovedPlan_ProfileKindRoundTripsAndReSigns proves Task 118 (SEC-04):
// the profile kind is an additive, re-signed provenance field that survives a
// marshal/verify round-trip.
func TestApprovedPlan_ProfileKindRoundTrips(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID: "p1", PlanDigest: "d1", RiskTier: admission.TierA2,
		ProfileKind: "organization",
		ApprovedAt:  time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if err := provenance.Sign(priv, ap); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if ap.ProfileKind() != "organization" {
		t.Fatalf("ProfileKind = %q, want organization", ap.ProfileKind())
	}
	raw, err := ap.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var got provenance.ApprovedPlan
	if err := got.UnmarshalJSON(raw); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if err := provenance.Verify(pub, &got); err != nil {
		t.Fatalf("Verify after round-trip: %v", err)
	}
	if got.ProfileKind() != "organization" {
		t.Fatalf("round-tripped ProfileKind = %q, want organization", got.ProfileKind())
	}
}

// TestApprovedPlan_EmptyProfileDefaultsPersonal proves backward compatibility:
// a plan approved before the field existed (empty) reads as personal and its
// signing payload is byte-identical to a plan that never set it.
func TestApprovedPlan_EmptyProfileDefaultsPersonal(t *testing.T) {
	ap, _ := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID: "p1", PlanDigest: "d1", RiskTier: admission.TierA0,
		ApprovedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if ap.ProfileKind() != "personal" {
		t.Fatalf("empty profile must default to personal, got %q", ap.ProfileKind())
	}
	payload, err := provenance.SigningPayload(ap)
	if err != nil {
		t.Fatal(err)
	}
	// The omitempty field must not appear in the payload of an empty-profile
	// plan, so an existing signed plan still verifies unchanged.
	if containsSub(string(payload), "profile_kind") {
		t.Fatal("empty profile_kind must be omitted from the signing payload (byte-identity with pre-Task-118 plans)")
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
