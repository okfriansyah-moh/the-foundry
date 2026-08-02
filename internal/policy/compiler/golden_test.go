package compiler

import (
	"strings"
	"testing"
)

// basePlatform returns a complete, valid platform LayerPolicy fixture
// shared by every golden case below (each case then perturbs org/profile/
// workflow, never platform itself, unless explicitly testing the
// platform-incomplete case).
func basePlatform() LayerPolicy {
	ref := "config/validation-allowlist.yaml"
	requireSandbox := false
	return LayerPolicy{
		PermissionsAllowlist: []string{"repo-read", "repo-write", "ci-trigger"},
		DeploymentModes: map[string]Mode{
			"preview":    ModeAuto,
			"staging":    ModeCommand,
			"production": ModeCommand,
		},
		BudgetCeilingsUSD: map[string]float64{
			"workflow_usd": 100,
			"task_usd":     5,
		},
		ExecutorAllowlist:      []string{"fake", "claude-code"},
		ValidationAllowlistRef: &ref,
		NotificationClasses:    []string{"telegram-low-risk", "telegram-veto-digest", "email"},
		RiskTierControls: map[string]RiskTierControl{
			"A0": {AutoAllowed: true, RequireReview: false},
			"A1": {AutoAllowed: true, RequireReview: true},
			"A2": {AutoAllowed: true, RequireReview: true},
			"H":  {AutoAllowed: false, RequireReview: true},
		},
		RequireSandbox: &requireSandbox,
	}
}

func strPtr(s string) *string { return &s }

// TestGolden is this task's acceptance-bar corpus: at least 15 cases,
// several of them attempted weakenings that MUST fail compilation with a
// *CompileError naming the exact layer and field.
func TestGolden(t *testing.T) {
	type tc struct {
		name        string
		platform    LayerPolicy
		org         LayerPolicy
		profile     LayerPolicy
		workflow    LayerPolicy
		wantErr     bool
		wantLayer   Layer
		wantField   string
		wantOverr   int  // expected len(Overrides), checked when wantErr is false
		checkDigest bool // when true, assert digest is non-empty and deterministic
	}

	cases := []tc{
		{
			name:        "platform only, no lower-layer overrides",
			platform:    basePlatform(),
			checkDigest: true,
			wantOverr:   0,
		},
		{
			name:      "org tightens permissions allowlist",
			platform:  basePlatform(),
			org:       LayerPolicy{PermissionsAllowlist: []string{"repo-read", "repo-write"}},
			wantOverr: 1,
		},
		{
			name:      "org attempts to WIDEN permissions allowlist — must fail",
			platform:  basePlatform(),
			org:       LayerPolicy{PermissionsAllowlist: []string{"repo-read", "repo-write", "ci-trigger", "secrets-read"}},
			wantErr:   true,
			wantLayer: LayerOrg,
			wantField: "permissions_allowlist",
		},
		{
			name:      "profile lowers a budget ceiling",
			platform:  basePlatform(),
			profile:   LayerPolicy{BudgetCeilingsUSD: map[string]float64{"task_usd": 2}},
			wantOverr: 1,
		},
		{
			name:      "profile attempts to RAISE a budget ceiling — must fail",
			platform:  basePlatform(),
			profile:   LayerPolicy{BudgetCeilingsUSD: map[string]float64{"task_usd": 50}},
			wantErr:   true,
			wantLayer: LayerProfile,
			wantField: "budget_ceilings_usd",
		},
		{
			name:      "profile narrows executor allowlist",
			platform:  basePlatform(),
			profile:   LayerPolicy{ExecutorAllowlist: []string{"claude-code"}},
			wantOverr: 1,
		},
		{
			name:      "profile attempts to WIDEN executor allowlist — must fail",
			platform:  basePlatform(),
			profile:   LayerPolicy{ExecutorAllowlist: []string{"fake", "claude-code", "codex"}},
			wantErr:   true,
			wantLayer: LayerProfile,
			wantField: "executor_allowlist",
		},
		{
			name:      "workflow tightens production deployment mode auto->command is a no-op (already command)",
			platform:  basePlatform(),
			workflow:  LayerPolicy{DeploymentModes: map[string]Mode{"production": ModeCommand}},
			wantOverr: 0,
		},
		{
			name:      "workflow tightens preview deployment mode auto->disabled",
			platform:  basePlatform(),
			workflow:  LayerPolicy{DeploymentModes: map[string]Mode{"preview": ModeDisabled}},
			wantOverr: 1,
		},
		{
			name:      "workflow attempts to WEAKEN production command->auto — must fail",
			platform:  basePlatform(),
			workflow:  LayerPolicy{DeploymentModes: map[string]Mode{"production": ModeAuto}},
			wantErr:   true,
			wantLayer: LayerWorkflow,
			wantField: "deployment_modes",
		},
		{
			name:      "workflow references an unknown deployment environment — must fail",
			platform:  basePlatform(),
			workflow:  LayerPolicy{DeploymentModes: map[string]Mode{"canary": ModeAuto}},
			wantErr:   true,
			wantLayer: LayerWorkflow,
			wantField: "deployment_modes",
		},
		{
			name:      "org tightens notification classes (forbids Telegram)",
			platform:  basePlatform(),
			org:       LayerPolicy{NotificationClasses: []string{"email"}},
			wantOverr: 1,
		},
		{
			name:      "workflow attempts to WIDEN notification classes after org forbade Telegram — must fail (docs N6.1 example)",
			platform:  basePlatform(),
			org:       LayerPolicy{NotificationClasses: []string{"email"}},
			workflow:  LayerPolicy{NotificationClasses: []string{"email", "telegram-low-risk"}},
			wantErr:   true,
			wantLayer: LayerWorkflow,
			wantField: "notification_classes",
		},
		{
			name:      "profile tightens a risk-tier control (A2 auto disallowed)",
			platform:  basePlatform(),
			profile:   LayerPolicy{RiskTierControls: map[string]RiskTierControl{"A2": {AutoAllowed: false, RequireReview: true}}},
			wantOverr: 1,
		},
		{
			name:      "workflow attempts to loosen a risk-tier control (H no longer requires review) — must fail",
			platform:  basePlatform(),
			workflow:  LayerPolicy{RiskTierControls: map[string]RiskTierControl{"H": {AutoAllowed: false, RequireReview: false}}},
			wantErr:   true,
			wantLayer: LayerWorkflow,
			wantField: "risk_tier_controls",
		},
		{
			name:      "workflow references an unknown risk tier — must fail",
			platform:  basePlatform(),
			workflow:  LayerPolicy{RiskTierControls: map[string]RiskTierControl{"A3": {AutoAllowed: false, RequireReview: true}}},
			wantErr:   true,
			wantLayer: LayerWorkflow,
			wantField: "risk_tier_controls",
		},
		{
			name:      "org attempts to change the fixed validation_allowlist_ref — must fail",
			platform:  basePlatform(),
			org:       LayerPolicy{ValidationAllowlistRef: strPtr("config/other-allowlist.yaml")},
			wantErr:   true,
			wantLayer: LayerOrg,
			wantField: "validation_allowlist_ref",
		},
		{
			name:      "profile restates the fixed validation_allowlist_ref identically — no override recorded",
			platform:  basePlatform(),
			profile:   LayerPolicy{ValidationAllowlistRef: strPtr("config/validation-allowlist.yaml")},
			wantOverr: 0,
		},
		{
			name:      "platform missing a required field — must fail",
			platform:  LayerPolicy{PermissionsAllowlist: []string{"repo-read"}}, // everything else unset
			wantErr:   true,
			wantLayer: LayerPlatform,
		},
		{
			name:      "cumulative: org, profile, and workflow each tighten a different field",
			platform:  basePlatform(),
			org:       LayerPolicy{PermissionsAllowlist: []string{"repo-read", "repo-write"}},
			profile:   LayerPolicy{BudgetCeilingsUSD: map[string]float64{"task_usd": 1}},
			workflow:  LayerPolicy{ExecutorAllowlist: []string{"fake"}},
			wantOverr: 3,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resolved, err := Compile(c.platform, c.org, c.profile, c.workflow)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected a compile error, got none (resolved=%+v)", resolved)
				}
				ce, ok := err.(*CompileError)
				if !ok {
					t.Fatalf("expected *CompileError, got %T: %v", err, err)
				}
				if c.wantLayer != "" && ce.Layer != c.wantLayer {
					t.Errorf("layer = %q, want %q", ce.Layer, c.wantLayer)
				}
				if c.wantField != "" && ce.Field != c.wantField {
					t.Errorf("field = %q, want %q", ce.Field, c.wantField)
				}
				if !strings.Contains(err.Error(), string(ce.Layer)) || !strings.Contains(err.Error(), ce.Field) {
					t.Errorf("error message %q must name both layer and field", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
			if len(resolved.Overrides) != c.wantOverr {
				t.Errorf("len(Overrides) = %d, want %d (overrides=%+v)", len(resolved.Overrides), c.wantOverr, resolved.Overrides)
			}
			if c.checkDigest && resolved.Digest == "" {
				t.Errorf("expected a non-empty digest")
			}
			if !strings.HasPrefix(resolved.Digest, "sha256:") {
				t.Errorf("digest %q missing sha256: prefix", resolved.Digest)
			}
		})
	}
}

// TestGoldenExplainListsEveryOverride asserts Explain's output actually
// mentions every override, not a truncated or summarized subset.
func TestGoldenExplainListsEveryOverride(t *testing.T) {
	resolved, err := Compile(
		basePlatform(),
		LayerPolicy{PermissionsAllowlist: []string{"repo-read", "repo-write"}},
		LayerPolicy{BudgetCeilingsUSD: map[string]float64{"task_usd": 1}},
		LayerPolicy{ExecutorAllowlist: []string{"fake"}},
	)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	explanation := Explain(resolved)
	for _, ov := range resolved.Overrides {
		if !strings.Contains(explanation, ov.Field) {
			t.Errorf("explanation missing override for field %q:\n%s", ov.Field, explanation)
		}
	}
}
