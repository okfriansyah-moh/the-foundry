package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// TestIntegration_HelloWorld runs the real `opencode` CLI. It is gated behind
// RUN_REAL_EXECUTOR=1 so it never runs in CI or a normal `go test ./...`
// pass (docs/PLAN.md Task 86 Validation) — it costs real API usage and
// requires `opencode` authenticated in the environment running it.
func TestIntegration_HelloWorld(t *testing.T) {
	if os.Getenv("RUN_REAL_EXECUTOR") != "1" {
		t.Skip("skipping: set RUN_REAL_EXECUTOR=1 to run the real opencode CLI (costs real API usage)")
	}
	if _, err := exec.LookPath(defaultBinary); err != nil {
		t.Skipf("skipping: %q not found on PATH: %v", defaultBinary, err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture repo for Task 86 integration test\n"), 0o644); err != nil {
		t.Fatalf("seed fixture repo: %v", err)
	}
	ws := worktree.Workspace{Path: dir}

	a := New()
	packet := executor.TaskPacket{
		PlanID:     "integration-fixture",
		TaskID:     "hello-world",
		Goal:       "Reply with exactly the text: hello world. Do not run any commands or edit any files.",
		TimeoutSec: 120,
	}
	if err := a.Prepare(context.Background(), ws, packet); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	summary, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v (Summary=%+v)", err, summary)
	}
	if summary.Claimed == "" {
		t.Fatalf("expected non-empty Claimed from a real run, got Summary=%+v", summary)
	}
	t.Logf("opencode integration Summary: %+v", summary)
}
