package compiler_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
)

// orgLayerFromYAML parses the org profile YAML into a LayerPolicy.
// Only the core policy fields are tested here; org_governance is
// a separate extension read by the governance layer directly.
func orgLayerFromYAML(t *testing.T, raw string) compiler.LayerPolicy {
	t.Helper()
	type orgYAML struct {
		PermissionsAllowlist []string                            `yaml:"permissions_allowlist"`
		DeploymentModes      map[string]compiler.Mode            `yaml:"deployment_modes"`
		BudgetCeilingsUSD    map[string]float64                  `yaml:"budget_ceilings_usd"`
		ExecutorAllowlist    []string                            `yaml:"executor_allowlist"`
		NotificationClasses  []string                            `yaml:"notification_classes"`
		RiskTierControls     map[string]compiler.RiskTierControl `yaml:"risk_tier_controls"`
	}
	var parsed orgYAML
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("parse org yaml: %v", err)
	}
	return compiler.LayerPolicy{
		PermissionsAllowlist: parsed.PermissionsAllowlist,
		DeploymentModes:      parsed.DeploymentModes,
		BudgetCeilingsUSD:    parsed.BudgetCeilingsUSD,
		ExecutorAllowlist:    parsed.ExecutorAllowlist,
		NotificationClasses:  parsed.NotificationClasses,
		RiskTierControls:     parsed.RiskTierControls,
	}
}

const org10xYAML = `
permissions_allowlist:
  - repo-read
  - repo-write
deployment_modes:
  preview: command
  staging: command
  production: command
budget_ceilings_usd:
  workflow_usd: 50
  task_usd: 2
executor_allowlist:
  - claude-code
notification_classes:
  - telegram-veto-digest
  - email
risk_tier_controls:
  A0:
    auto_allowed: true
    require_review: false
  A1:
    auto_allowed: false
    require_review: true
  A2:
    auto_allowed: false
    require_review: true
  H:
    auto_allowed: false
    require_review: true
`

// TestOrgProfileCompiles verifies the org 10x profile compiles successfully.
func TestOrgProfileCompiles(t *testing.T) {
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("load platform: %v", err)
	}
	org := orgLayerFromYAML(t, org10xYAML)
	_, err = compiler.Compile(platform, org, compiler.LayerPolicy{}, compiler.LayerPolicy{})
	if err != nil {
		t.Fatalf("Compile with org 10x profile: %v", err)
	}
}

// TestOrgProfileTightensA1 verifies A1 auto_allowed=false (tighter than platform).
func TestOrgProfileTightensA1(t *testing.T) {
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("load platform: %v", err)
	}
	org := orgLayerFromYAML(t, org10xYAML)
	resolved, err := compiler.Compile(platform, org, compiler.LayerPolicy{}, compiler.LayerPolicy{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	a1 := resolved.Effective.RiskTierControls["A1"]
	if a1.AutoAllowed {
		t.Error("A1 auto_allowed=true, want false at org layer")
	}
	if !a1.RequireReview {
		t.Error("A1 require_review=false, want true at org layer")
	}
}

// TestOrgProfileWeakeningFails verifies that attempting to widen A2 auto_allowed
// at the org layer (setting it true while platform has it true and org wants
// it false — testing the other direction: org allows A2 auto when platform
// tightened it) fails compilation.
func TestOrgProfileWeakeningFails(t *testing.T) {
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("load platform: %v", err)
	}
	// First apply a stricter-than-platform org layer (A2 auto_allowed=false).
	org := orgLayerFromYAML(t, org10xYAML)
	// Now try a profile that widens it back to true — must fail.
	profileWiden := compiler.LayerPolicy{
		RiskTierControls: map[string]compiler.RiskTierControl{
			"A2": {AutoAllowed: true, RequireReview: false},
		},
	}
	_, err = compiler.Compile(platform, org, profileWiden, compiler.LayerPolicy{})
	if err == nil {
		t.Error("Compile should fail when profile widens A2 auto_allowed above org layer, got nil")
	}
}

// TestOrgProfileBudgetTightened verifies org task budget is <= platform ceiling.
func TestOrgProfileBudgetTightened(t *testing.T) {
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("load platform: %v", err)
	}
	org := orgLayerFromYAML(t, org10xYAML)
	resolved, err := compiler.Compile(platform, org, compiler.LayerPolicy{}, compiler.LayerPolicy{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if resolved.Effective.BudgetCeilingsUSD["task_usd"] > 5 {
		t.Errorf("task_usd=%.2f, want <= 5 (platform ceiling)", resolved.Effective.BudgetCeilingsUSD["task_usd"])
	}
}

// TestOrgProfileExecutorNarrowed verifies executor allowlist is a strict
// subset of the platform list (tighten-only).
func TestOrgProfileExecutorNarrowed(t *testing.T) {
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("load platform: %v", err)
	}
	org := orgLayerFromYAML(t, org10xYAML)
	resolved, err := compiler.Compile(platform, org, compiler.LayerPolicy{}, compiler.LayerPolicy{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// "fake" should not be in org executor allowlist.
	for _, ex := range resolved.Effective.ExecutorAllowlist {
		if ex == "fake" {
			t.Error("executor allowlist contains 'fake' — org layer should have narrowed it out")
		}
	}
}

// TestPDPDeniesNonKernelPushAuthorization verifies the governance rule that
// only go-kernel principals may be granted push authorization.
// This is an API-shape test: the PDP denies a non-kernel push attempt.
func TestPDPDeniesNonKernelPushAuthorization(t *testing.T) {
	// push_authorization=kernel-only means the org governance layer denies
	// any principal that is not "service:go-kernel".
	govPack := compiler.OrgGovernancePack{
		PushAuthorization:     "kernel-only",
		RequiredApproverRoles: []string{"engineering", "qa"},
	}
	if govPack.AllowsPushBy("service:go-backend") {
		t.Error("AllowsPushBy returned true for go-backend — only go-kernel allowed")
	}
	if !govPack.AllowsPushBy("service:go-kernel") {
		t.Error("AllowsPushBy returned false for go-kernel — should be allowed")
	}
}
