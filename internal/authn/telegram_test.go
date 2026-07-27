package authn_test

import (
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

func secureSurfaceFor(planID string) string { return "https://approve.example.com/plans/" + planID }

// TestTelegramApprove_RejectsHighTier is the C11 test this task's card
// names explicitly: "Telegram must never be a High-risk approval
// surface."
func TestTelegramApprove_RejectsHighTier(t *testing.T) {
	result := authn.TelegramApprove("plan-1", authn.PlanContext{Tier: admission.TierH}, secureSurfaceFor)
	if result.Allowed {
		t.Fatal("expected a High-tier Telegram approve to be rejected")
	}
	if !strings.Contains(result.Reply, "high-risk approval requires the secure surface") {
		t.Fatalf("reply = %q, want it to point at the secure surface", result.Reply)
	}
	if !strings.Contains(result.Reply, secureSurfaceFor("plan-1")) {
		t.Fatalf("reply = %q, want it to contain the secure surface URL", result.Reply)
	}
}

func TestTelegramApprove_RejectsOrganizationProfile(t *testing.T) {
	result := authn.TelegramApprove("plan-2", authn.PlanContext{Tier: admission.TierA0, Profile: profile.Organization}, secureSurfaceFor)
	if result.Allowed {
		t.Fatal("expected an organization-profile Telegram approve to be rejected even at low tier")
	}
}

func TestTelegramApprove_AllowsLowRiskPersonal(t *testing.T) {
	result := authn.TelegramApprove("plan-3", authn.PlanContext{Tier: admission.TierA1, Profile: profile.Personal}, secureSurfaceFor)
	if !result.Allowed {
		t.Fatalf("expected a low-tier personal-profile Telegram approve to be allowed, got reply %q", result.Reply)
	}
}
