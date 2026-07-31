package compiler_test

import (
	"os"
	"path/filepath"
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
