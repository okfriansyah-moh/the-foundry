package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
	"github.com/okfriansyah-moh/the-foundry/test/fixtures"
)

func TestAcquireCreatesIsolatedWorktree(t *testing.T) {
	repo := fixtures.NewRepo(t)
	m := &worktree.Manager{Root: t.TempDir()}

	ws, err := m.Acquire(context.Background(), repo, "wf1", "task1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer ws.Release()

	if ws.Branch != "foundry/wf1/task1" {
		t.Fatalf("branch = %q, want foundry/wf1/task1", ws.Branch)
	}
	if info, err := os.Stat(ws.Path); err != nil || !info.IsDir() {
		t.Fatalf("workspace path %s missing: %v", ws.Path, err)
	}

	out, err := exec.Command("git", "-C", ws.Path, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse in worktree: %v: %s", err, out)
	}
	if got := string(out); got != "foundry/wf1/task1\n" {
		t.Fatalf("worktree HEAD branch = %q", got)
	}
}

func TestAcquireRejectsPathTraversal(t *testing.T) {
	repo := fixtures.NewRepo(t)
	m := &worktree.Manager{Root: t.TempDir()}

	cases := []struct{ wfID, taskID string }{
		{"../escape", "task1"},
		{"wf1", "../../escape"},
		{"wf/1", "task1"},
		{"", "task1"},
		{"wf1", ""},
	}
	for _, c := range cases {
		if _, err := m.Acquire(context.Background(), repo, c.wfID, c.taskID); err == nil {
			t.Fatalf("Acquire(%q, %q): expected error, got nil", c.wfID, c.taskID)
		}
	}
}

func TestReleaseRemovesWorktreeAndBranch(t *testing.T) {
	repo := fixtures.NewRepo(t)
	m := &worktree.Manager{Root: t.TempDir()}

	ws, err := m.Acquire(context.Background(), repo, "wf1", "task1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := ws.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("workspace path %s should be gone, stat err = %v", ws.Path, err)
	}

	out, err := exec.Command("git", "-C", repo, "branch", "--list", ws.Branch).CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --list: %v: %s", err, out)
	}
	if len(out) != 0 {
		t.Fatalf("branch %s should be deleted, git branch --list output: %s", ws.Branch, out)
	}
}

// TestConcurrentAcquireRelease is the acceptance bar from docs/PLAN.md
// Task 9: 10 concurrent tasks against the same repo, zero path collisions,
// run under `-race -count=10`.
func TestConcurrentAcquireRelease(t *testing.T) {
	repo := fixtures.NewRepo(t)
	m := &worktree.Manager{Root: t.TempDir()}

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	paths := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			taskID := "task" + string(rune('a'+i))
			ws, err := m.Acquire(context.Background(), repo, "wf-concurrent", taskID)
			if err != nil {
				errs[i] = err
				return
			}
			paths[i] = ws.Path
			// Simulate agent work: write into the isolated worktree only.
			if werr := os.WriteFile(filepath.Join(ws.Path, "output.txt"), []byte(taskID), 0o644); werr != nil {
				errs[i] = werr
				return
			}
			errs[i] = ws.Release()
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("task %d: %v", i, err)
		}
		if seen[paths[i]] {
			t.Fatalf("path collision: %s used by more than one task", paths[i])
		}
		seen[paths[i]] = true
	}
}

// TestCanonicalRepoUntouched proves parallel acquires+writes never mutate
// the canonical repository itself (docs/PLAN.md Task 9 Step 4).
func TestCanonicalRepoUntouched(t *testing.T) {
	repo := fixtures.NewRepo(t)
	m := &worktree.Manager{Root: t.TempDir()}

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			taskID := "task" + string(rune('a'+i))
			ws, err := m.Acquire(context.Background(), repo, "wf-canon", taskID)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			_ = os.WriteFile(filepath.Join(ws.Path, "agent-work.txt"), []byte("hello"), 0o644)
			addCmd := exec.Command("git", "-C", ws.Path, "add", "agent-work.txt")
			_ = addCmd.Run()
			commitCmd := exec.Command("git", "-C", ws.Path, "commit", "-m", "agent commit "+taskID)
			_ = commitCmd.Run()
			if err := ws.Release(); err != nil {
				t.Errorf("Release: %v", err)
			}
		}(i)
	}
	wg.Wait()

	statusOut, err := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain: %v: %s", err, statusOut)
	}
	if len(statusOut) != 0 {
		t.Fatalf("canonical repo dirty: %s", statusOut)
	}

	fsckOut, err := exec.Command("git", "-C", repo, "fsck", "--full").CombinedOutput()
	if err != nil {
		t.Fatalf("git fsck: %v: %s", err, fsckOut)
	}
}

func TestSweepOlderThanRemovesOrphans(t *testing.T) {
	repo := fixtures.NewRepo(t)
	root := t.TempDir()
	m := &worktree.Manager{Root: root}

	ws, err := m.Acquire(context.Background(), repo, "wf1", "orphan")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// SweepOlderThan(0) treats every existing worktree as older than "now".
	if err := m.SweepOlderThan(0); err != nil {
		t.Fatalf("SweepOlderThan: %v", err)
	}

	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("orphaned workspace %s should have been swept, stat err = %v", ws.Path, err)
	}
}

func TestSweepOlderThanKeepsRecent(t *testing.T) {
	repo := fixtures.NewRepo(t)
	root := t.TempDir()
	m := &worktree.Manager{Root: root}

	ws, err := m.Acquire(context.Background(), repo, "wf1", "recent")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer ws.Release()

	if err := m.SweepOlderThan(time.Hour); err != nil {
		t.Fatalf("SweepOlderThan: %v", err)
	}

	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("recent workspace %s should survive sweep: %v", ws.Path, err)
	}
}

func TestAcquireRejectsNonexistentRepo(t *testing.T) {
	m := &worktree.Manager{Root: t.TempDir()}
	if _, err := m.Acquire(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), "wf1", "task1"); err == nil {
		t.Fatal("expected error for nonexistent repo path")
	}
}
