package fake

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func fixture(name string) string {
	return filepath.Join("..", "..", "..", "test", "fixtures", "fake_scripts", name)
}

func TestFake_Success(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	f := New()

	if err := f.Prepare(context.Background(), ws, executor.TaskPacket{Goal: fixture("success.yaml")}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Claimed != "all tests pass" {
		t.Fatalf("unexpected Claimed: %q", summary.Claimed)
	}

	got, err := os.ReadFile(filepath.Join(ws.Path, "output.txt"))
	if err != nil {
		t.Fatalf("patch not applied: %v", err)
	}
	if string(got) != "hello from fake executor\n" {
		t.Fatalf("unexpected patch content: %q", got)
	}

	artifacts, err := f.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(artifacts.Paths) != 1 || artifacts.Paths[0] != "output.txt" {
		t.Fatalf("unexpected artifacts: %+v", artifacts)
	}
}

func TestFake_Fail(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	f := New()

	if err := f.Prepare(context.Background(), ws, executor.TaskPacket{Goal: fixture("fail.yaml")}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := f.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error for nonzero exit_code")
	}
	if summary.Claimed != "tests failed" {
		t.Fatalf("unexpected Claimed: %q", summary.Claimed)
	}
}

// TestFake_Lie proves the fake can emit a Summary that contradicts what
// actually happened: exit_code is nonzero (real failure) while Claimed
// reports success. Task 13's honest-completion tests rely on this to prove
// the kernel never trusts an executor's self-report.
func TestFake_Lie(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	f := New()

	if err := f.Prepare(context.Background(), ws, executor.TaskPacket{Goal: fixture("lie.yaml")}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, runErr := f.Run(context.Background())
	if runErr == nil {
		t.Fatalf("expected the underlying run to have actually failed")
	}
	if summary.Claimed != "all tests pass" {
		t.Fatalf("expected lying Claimed summary, got %q", summary.Claimed)
	}
}

func TestFake_Timeout(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	f := New()

	if err := f.Prepare(context.Background(), ws, executor.TaskPacket{Goal: fixture("timeout.yaml")}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := f.Run(ctx); err == nil {
		t.Fatalf("expected context deadline error")
	}
}

func TestFake_PrepareRequiresGoal(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	f := New()
	if err := f.Prepare(context.Background(), ws, executor.TaskPacket{}); err == nil {
		t.Fatalf("expected error when packet.Goal is empty")
	}
}

func TestGetRegisteredFake(t *testing.T) {
	adapter, err := executor.Get("fake")
	if err != nil {
		t.Fatalf("Get(\"fake\"): %v", err)
	}
	if _, ok := adapter.(*Fake); !ok {
		t.Fatalf("Get(\"fake\") returned wrong type: %T", adapter)
	}
}
