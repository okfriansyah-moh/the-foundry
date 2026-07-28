package local

import (
	"context"
	"os"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// TestIntegration_HelloWorld hits the real provider endpoint. Gated behind
// RUN_REAL_EXECUTOR=1 (docs/PLAN.md Task 79 Validation) — costs real API
// usage (openai) or needs a running local endpoint (local).
func TestIntegration_HelloWorld(t *testing.T) {
	if os.Getenv("RUN_REAL_EXECUTOR") != "1" {
		t.Skip("skipping: set RUN_REAL_EXECUTOR=1 to run against the real local endpoint")
	}
	a := New()
	ws := worktree.Workspace{Path: t.TempDir()}
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "Reply with exactly: hello world", TimeoutSec: 60}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v (Summary=%+v)", err, summary)
	}
	if summary.Claimed == "" {
		t.Fatalf("expected non-empty Claimed, got %+v", summary)
	}
	t.Logf("local integration Summary: %+v", summary)
}
