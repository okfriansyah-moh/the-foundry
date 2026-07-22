package provenance_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// TestGrantedIsAlwaysSubsetOfRequested_ByConstruction exercises Constitution
// C7 rule 2 ("granted permissions are always the policy-validated subset")
// through the type's only construction path — there is no setter that
// could assign Granted directly, so this test is really asserting that no
// such setter exists and that the one path there is behaves correctly.
func TestGrantedIsAlwaysSubsetOfRequested_ByConstruction(t *testing.T) {
	doc := mustParseFixturePlan(t)
	allow := mustLoadAllowList(t) // allows repo-read, not billing-write

	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     doc.ID,
		PlanDigest: doc.DigestHex(),
		Requested:  doc.RequestedPermissions,
	}, allow)
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}

	requested := ap.Requested()
	granted := ap.Granted()

	if len(requested) != 2 {
		t.Fatalf("fixture should request 2 permissions, got %d", len(requested))
	}
	if len(granted) != 1 {
		t.Fatalf("allowlist should grant exactly 1 of 2 requested, got %d: %+v", len(granted), granted)
	}
	if granted[0].Kind != "repo-read" {
		t.Fatalf("expected granted permission repo-read, got %q", granted[0].Kind)
	}

	set := make(map[plan.Permission]struct{}, len(requested))
	for _, r := range requested {
		set[r] = struct{}{}
	}
	for _, g := range granted {
		if _, ok := set[g]; !ok {
			t.Fatalf("granted permission %+v is not in requested — C7 rule 2 violated", g)
		}
	}
}

// TestNewApprovedPlan_EmptyAllowListGrantsNothing confirms Granted can be
// empty (not merely "smaller") when the allowlist covers none of the
// requested permissions.
func TestNewApprovedPlan_EmptyAllowListGrantsNothing(t *testing.T) {
	doc := mustParseFixturePlan(t)

	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     doc.ID,
		PlanDigest: doc.DigestHex(),
		Requested:  doc.RequestedPermissions,
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}
	if len(ap.Granted()) != 0 {
		t.Fatalf("expected 0 granted permissions against an empty allowlist, got %d", len(ap.Granted()))
	}
}

func TestNewApprovedPlan_RequiresPlanIDAndDigest(t *testing.T) {
	cases := []provenance.ApprovedPlanInput{
		{PlanID: "", PlanDigest: "deadbeef"},
		{PlanID: "plan-1", PlanDigest: ""},
	}
	for _, c := range cases {
		if _, err := provenance.NewApprovedPlan(c, provenance.AllowList{}); err == nil {
			t.Fatalf("expected error for input %+v", c)
		}
	}
}
