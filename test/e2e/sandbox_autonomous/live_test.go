package sandboxautonomous_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func TestSandboxEligibilityMatrix(t *testing.T) {
	reg, err := capability.Load(filepath.Join("..", "..", "..", "config", "executor-capabilities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range reg.EligibleForSandbox("personal", nil) {
		if !rec.IsSandboxSupported() {
			t.Fatalf("%s in EligibleForSandbox but not sandbox_supported", rec.Provider)
		}
	}
	if got := reg.EligibleForSandbox("personal", nil); len(got) == 0 {
		t.Fatal("expected at least one sandbox_supported executor")
	}
	// host_only local must not appear when filtering for sandbox.
	for _, rec := range reg.EligibleForSandbox("personal", nil) {
		if rec.Provider == "local" {
			t.Fatal("host_only local must not be EligibleForSandbox")
		}
	}
}

func TestFakeAdapterProvidesSandboxSpec(t *testing.T) {
	a := fake.New()
	provider, ok := any(a).(executor.SandboxSpecProvider)
	if !ok {
		t.Fatal("fake adapter must implement SandboxSpecProvider")
	}
	spec, err := provider.SandboxSpec(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Argv) == 0 {
		t.Fatal("empty argv")
	}
}
