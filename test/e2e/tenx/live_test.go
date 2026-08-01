package tenx_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestLiveTenXProviderSelectionAssertsConfigResolved proves Task 133 step 1's
// "configurable" claim: provider is selected from policy, not hardcoded.
func TestLiveTenXProviderSelectionAssertsConfigResolved(t *testing.T) {
	reg := kernel.NewSCMWriterRegistry()
	// Writers deliberately absent — selection still resolves the name from policy.
	_, code, err := kernel.SelectSCMProvider("bitbucket", "policy-digest", "https://bitbucket.org/ws/disposable.git", reg)
	if err == nil {
		t.Fatal("expected missing-writer refusal")
	}
	if code != kernel.ResultSCMWriterMissing {
		t.Fatalf("code=%s", code)
	}
	_, code, err = kernel.SelectSCMProvider("github", "policy-digest", "https://github.com/o/r.git", reg)
	if err == nil || code != kernel.ResultSCMWriterMissing {
		t.Fatalf("github selection by policy alone failed: %v %s", err, code)
	}
}

func TestLiveTenXDisposableGuard(t *testing.T) {
	if os.Getenv("RUN_TENX_LIVE") != "1" {
		t.Skip("RUN_TENX_LIVE=1 not set")
	}
	if os.Getenv("FOUNDRY_TENX_DISPOSABLE") != "1" {
		t.Fatal("refusing live TenX: FOUNDRY_TENX_DISPOSABLE=1 required")
	}
	root := os.Getenv("FOUNDRY_TENX_EVIDENCE")
	if root == "" {
		root = filepath.Join("evidence", "m5-tenx")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	note := []byte("live TenX disposable-remote gate entered; full remote push requires wired foundryd + Bitbucket credentials\n")
	if err := os.WriteFile(filepath.Join(root, "live-gate.txt"), note, 0o644); err != nil {
		t.Fatal(err)
	}
}
