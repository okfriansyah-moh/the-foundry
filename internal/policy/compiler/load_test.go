package compiler_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
)

func TestLoadOrgLayer_RealConfig(t *testing.T) {
	layer, pack, err := compiler.LoadOrgLayer("../../../config/profiles/organization-10x.yaml")
	if err != nil {
		t.Fatalf("LoadOrgLayer: %v", err)
	}
	if pack.PushAuthorization != "kernel-only" {
		t.Fatalf("org governance push_authorization = %q, want kernel-only", pack.PushAuthorization)
	}
	if len(layer.ExecutorAllowlist) == 0 {
		t.Fatal("org layer must carry its tighter executor allowlist")
	}
	// The kernel-only push rule must be in force.
	if pack.AllowsPushBy("service:some-backend") {
		t.Fatal("kernel-only governance must deny a non-kernel pusher")
	}
	if !pack.AllowsPushBy("service:go-kernel") {
		t.Fatal("kernel-only governance must permit go-kernel")
	}
}

func TestLoadProfileLayer_PersonalConfig(t *testing.T) {
	layer, err := compiler.LoadProfileLayer("../../../config/profiles/personal-autonomous-venture.yaml")
	if err != nil {
		t.Fatalf("LoadProfileLayer: %v", err)
	}
	if layer.BudgetCeilingsUSD["workflow_usd"] != 50 {
		t.Fatalf("personal profile workflow ceiling = %v, want 50", layer.BudgetCeilingsUSD["workflow_usd"])
	}
}

func TestLoadProfileLayer_RejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("some_unmapped_field: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.LoadProfileLayer(p); err == nil {
		t.Fatal("an unmapped policy-meaning key must reject (strict schema), not be silently dropped")
	}
}

func TestLoadProfileLayer_RejectsInvalidPackageDeclarations(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "unknown agent package field",
			raw:  "agent_packages:\n  enabled: [planning]\n  executor_allowlist: [fake]\n",
		},
		{
			name: "agent package domain enablement",
			raw:  "agent_packages:\n  enabled: [planning]\n  domain_enabled: [commercial-readiness]\n",
		},
		{
			name: "unknown nested skill package field",
			raw:  "skill_packages:\n  enabled: [testing]\n  runtime:\n    executor: fake\n",
		},
		{
			name: "duplicate package mapping",
			raw:  "agent_packages:\n  enabled: [planning]\nagent_packages:\n  enabled: [reviewer]\n",
		},
		{
			name: "duplicate nested package field",
			raw:  "skill_packages:\n  enabled: [testing]\n  enabled: [guardrails]\n",
		},
		{
			name: "nested value where package name is required",
			raw:  "agent_packages:\n  enabled:\n    - name: planning\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.yaml")
			if err := os.WriteFile(path, []byte(tt.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := compiler.LoadProfileLayer(path); err == nil {
				t.Fatal("invalid package declaration must fail closed")
			}
		})
	}
}

func TestPackageDeclarationsDoNotAlterResolvedPolicy(t *testing.T) {
	dir := t.TempDir()
	withoutPackages := filepath.Join(dir, "without-packages.yaml")
	withPackages := filepath.Join(dir, "with-packages.yaml")
	if err := os.WriteFile(withoutPackages, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(withPackages, []byte(`agent_packages:
  enabled:
    - repo-write
    - production-deploy
skill_packages:
  enabled:
    - executor-allowlist
  domain_enabled:
    - kernel-authority
`), 0o600); err != nil {
		t.Fatal(err)
	}

	withoutResolved, withoutPack, err := compiler.CompileFourLayer("", withoutPackages)
	if err != nil {
		t.Fatalf("CompileFourLayer without package declarations: %v", err)
	}
	withResolved, withPack, err := compiler.CompileFourLayer("", withPackages)
	if err != nil {
		t.Fatalf("CompileFourLayer with package declarations: %v", err)
	}
	if !reflect.DeepEqual(withResolved, withoutResolved) {
		t.Fatalf("package declarations altered resolved policy:\nwith:    %+v\nwithout: %+v", withResolved, withoutResolved)
	}
	if !reflect.DeepEqual(withPack, withoutPack) {
		t.Fatalf("package declarations altered org governance: with=%+v without=%+v", withPack, withoutPack)
	}
}

func TestLoadProfileLayer_RejectsOrgGovernance(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(p, []byte("org_governance:\n  push_authorization: kernel-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.LoadProfileLayer(p); err == nil {
		t.Fatal("a profile layer must not declare org_governance (org-layer-only)")
	}
}

func TestCompileFourLayer_OrgTightensPlatform(t *testing.T) {
	resolved, pack, err := compiler.CompileFourLayer("../../../config/profiles/organization-10x.yaml", "")
	if err != nil {
		t.Fatalf("CompileFourLayer: %v", err)
	}
	if resolved.Digest == "" {
		t.Fatal("four-layer compile must produce a digest")
	}
	if pack.PushAuthorization != "kernel-only" {
		t.Fatal("org governance pack must be returned from the four-layer compile")
	}
	// The org layer's tighter workflow ceiling (50) must be in force, not the
	// platform default (100).
	if resolved.Effective.BudgetCeilingsUSD["workflow_usd"] != 50 {
		t.Fatalf("org ceiling not in force: %v", resolved.Effective.BudgetCeilingsUSD["workflow_usd"])
	}
}

func TestCompileFourLayer_BadPathIsHardError(t *testing.T) {
	if _, _, err := compiler.CompileFourLayer("/no/such/org.yaml", ""); err == nil {
		t.Fatal("a non-empty org path that fails to load must be a hard error, not a silent skip")
	}
}
