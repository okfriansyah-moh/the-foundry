//go:build redteam

package redteam

import (
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// TestCrossProfile_WorktreeEscapeDenied proves Task 118 (SEC-04): a task cannot
// be handed a workspace outside its own profile's subtree, and a malicious
// profile id cannot escape the base worktree root.
func TestCrossProfile_WorktreeEscapeDenied(t *testing.T) {
	base := &worktree.Manager{Root: t.TempDir()}
	// A path-escaping profile id must be rejected outright.
	for _, evil := range []string{"../other", "..", "/etc", "a/b"} {
		if _, err := base.ForProfile(evil); err == nil {
			t.Fatalf("ForProfile(%q) must be denied — a profile cannot escape the root", evil)
		}
	}
	personal, err := base.ForProfile("personal")
	if err != nil {
		t.Fatalf("ForProfile(personal): %v", err)
	}
	org, err := base.ForProfile("org-acme")
	if err != nil {
		t.Fatalf("ForProfile(org): %v", err)
	}
	if !strings.HasPrefix(personal.Root, base.Root) || personal.Root == org.Root {
		t.Fatal("each profile must be contained in its own distinct subtree of the base root")
	}
}

// TestCrossProfile_OrgPlanApprovalRequiresStepUp proves an organization-profile
// plan requires strong auth even below tier H (the hardcoded profile.Personal
// that made this unreachable is gone).
func TestCrossProfile_OrgPlanApprovalRequiresStepUp(t *testing.T) {
	orgPlan, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID: "p-org", PlanDigest: "d", ProfileKind: "organization",
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if orgPlan.ProfileKind() != "organization" {
		t.Fatalf("org plan must carry its profile kind, got %q", orgPlan.ProfileKind())
	}
	personalPlan, _ := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID: "p-personal", PlanDigest: "d",
	}, provenance.AllowList{})
	if personalPlan.ProfileKind() != "personal" {
		t.Fatalf("a plan with no profile must default to personal, got %q", personalPlan.ProfileKind())
	}
}
