package contracttest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// Options configures the shared adapter contract suite for one provider.
type Options struct {
	// Name is the registry name the adapter registers under (executor.Get).
	Name string
	// New constructs a fresh adapter. It is called AFTER any test env var is
	// set, so BinEnvOverride is honored.
	New func() executor.Adapter
	// BinEnvOverride is the env var a test sets to point the adapter's binary
	// at a stub executable (e.g. "FOUNDRY_OPENCODE_BIN").
	BinEnvOverride string
	// AuthEnvVar, when non-empty, is the one credential var that MUST pass
	// through the adapter's env allowlist (the provider's own auth). The
	// leak test asserts it survives while unrelated secrets are scrubbed.
	AuthEnvVar string
}

// Run executes the full contract suite as subtests of t.
func Run(t *testing.T, o Options) {
	t.Helper()
	t.Run("Registered", func(t *testing.T) { testRegistered(t, o) })
	t.Run("PromptFileJailedToWorkspace", func(t *testing.T) { testPromptJailed(t, o) })
	t.Run("RejectsEmptyWorkspaceOrGoal", func(t *testing.T) { testRejects(t, o) })
	t.Run("RunEchoesSummary", func(t *testing.T) { testRunEcho(t, o) })
	t.Run("NoSecretLeak", func(t *testing.T) { testNoSecretLeak(t, o) })
	t.Run("NonzeroExitIsError", func(t *testing.T) { testNonzeroExit(t, o) })
	t.Run("TimeoutKillsSubprocess", func(t *testing.T) { testTimeout(t, o) })
}

// LeakCheck proves the fresh-context-per-invocation policy (docs/PLAN.md
// Task 91 / PRV-08, authority-model.md N7.5) for o's adapter: two instances
// from the same constructor, prepared concurrently in separate workspaces
// under the race detector, must not share mutable state — each workspace
// must contain only its own task's content. It fails the test on violation.
func LeakCheck(t *testing.T, o Options) {
	t.Helper()
	if err := leakViolation(o.New); err != nil {
		t.Fatalf("fresh-context contract violated for %q: %v", o.Name, err)
	}
}

// leakViolation runs the isolation probe and returns a non-nil error iff the
// constructor's adapters leak state across instances. Factored out from
// LeakCheck so leak_test.go can assert the check bites on a planted leaky
// fixture (returns error) and passes on a clean adapter (returns nil).
func leakViolation(newAdapter func() executor.Adapter) error {
	dir, err := os.MkdirTemp("", "contractleak")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	const goalA, goalB = "GOAL-AAA-isolation-canary", "GOAL-BBB-isolation-canary"
	wsA := filepath.Join(dir, "a")
	wsB := filepath.Join(dir, "b")
	for _, d := range []string{wsA, wsB} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return err
		}
	}

	a, b := newAdapter(), newAdapter()
	var wg sync.WaitGroup
	var errA, errB error
	wg.Add(2)
	go func() {
		defer wg.Done()
		errA = a.Prepare(context.Background(), worktree.Workspace{Path: wsA}, executor.TaskPacket{Goal: goalA})
	}()
	go func() {
		defer wg.Done()
		errB = b.Prepare(context.Background(), worktree.Workspace{Path: wsB}, executor.TaskPacket{Goal: goalB})
	}()
	wg.Wait()
	if errA != nil {
		return fmt.Errorf("Prepare(A): %w", errA)
	}
	if errB != nil {
		return fmt.Errorf("Prepare(B): %w", errB)
	}

	contentA, contentB := readTree(wsA), readTree(wsB)
	if !strings.Contains(contentA, goalA) {
		return fmt.Errorf("workspace A does not contain its own goal (state leaked out): %q", contentA)
	}
	if strings.Contains(contentA, goalB) {
		return fmt.Errorf("workspace A contains the OTHER task's goal (cross-task leak)")
	}
	if !strings.Contains(contentB, goalB) {
		return fmt.Errorf("workspace B does not contain its own goal (state leaked out): %q", contentB)
	}
	if strings.Contains(contentB, goalA) {
		return fmt.Errorf("workspace B contains the OTHER task's goal (cross-task leak)")
	}
	return nil
}

// readTree concatenates every regular file's content under root.
func readTree(root string) string {
	var b strings.Builder
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil {
			b.Write(data)
		}
		return nil
	})
	return b.String()
}

func testRegistered(t *testing.T, o Options) {
	a, err := executor.Get(o.Name)
	if err != nil {
		t.Fatalf("executor.Get(%q): %v", o.Name, err)
	}
	if a == nil {
		t.Fatalf("executor.Get(%q) returned nil adapter", o.Name)
	}
}

// writeStub writes an executable /bin/sh stub and returns its path.
func writeStub(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stub.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return p
}

// testPromptJailed proves no packet field (a path-traversal TaskID) can make
// Prepare write outside the workspace directory.
func testPromptJailed(t *testing.T, o Options) {
	parent := t.TempDir()
	wsPath := filepath.Join(parent, "ws")
	if err := os.Mkdir(wsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	a := o.New()
	packet := executor.TaskPacket{
		PlanID: "plan-1",
		TaskID: "../../etc/passwd", // must not influence the path at all
		Goal:   "say hello",
	}
	if err := a.Prepare(context.Background(), worktree.Workspace{Path: wsPath}, packet); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	// Every regular file created must live under wsPath; nothing escaped.
	err := filepath.Walk(parent, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasPrefix(path, wsPath+string(os.PathSeparator)) {
			t.Fatalf("file written outside workspace: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testRejects(t *testing.T, o Options) {
	if err := o.New().Prepare(context.Background(), worktree.Workspace{}, executor.TaskPacket{Goal: "x"}); err == nil {
		t.Fatal("expected error for empty workspace path")
	}
	if err := o.New().Prepare(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{}); err == nil {
		t.Fatal("expected error for empty goal")
	}
}

func testRunEcho(t *testing.T, o Options) {
	stub := writeStub(t, `echo "contract-ok"`)
	t.Setenv(o.BinEnvOverride, stub)
	a := o.New()
	if err := a.Prepare(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(summary.Claimed, "contract-ok") {
		t.Fatalf("Summary.Claimed = %q, want it to contain the stub output", summary.Claimed)
	}
	arts, err := a.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(arts.Paths) == 0 {
		t.Fatal("Collect returned no artifacts")
	}
}

func testNoSecretLeak(t *testing.T, o Options) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "env_dump.txt")
	stub := writeStub(t, "env > "+dump+"\necho ok\n")
	t.Setenv(o.BinEnvOverride, stub)
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_leak_me_not")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak_me_not")
	t.Setenv("DATABASE_URL", "postgres://leak_me_not")
	if o.AuthEnvVar != "" {
		t.Setenv(o.AuthEnvVar, "auth-should-be-visible")
	}
	a := o.New()
	if err := a.Prepare(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	got := string(b)
	// Note: GITHUB_TOKEN is deliberately NOT a canary here — a GitHub-auth
	// provider (copilot) legitimately allowlists it. The remaining canaries
	// plus the shared "leak_me_not" value robustly prove env scrubbing.
	for _, secret := range []string{"STRIPE_SECRET_KEY", "AWS_SECRET_ACCESS_KEY", "DATABASE_URL", "leak_me_not"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret-like var %q leaked into %s subprocess env:\n%s", secret, o.Name, got)
		}
	}
	if o.AuthEnvVar != "" && !strings.Contains(got, o.AuthEnvVar+"=auth-should-be-visible") {
		t.Fatalf("required auth var %q missing from subprocess env:\n%s", o.AuthEnvVar, got)
	}
}

func testNonzeroExit(t *testing.T, o Options) {
	stub := writeStub(t, `echo boom; exit 1`)
	t.Setenv(o.BinEnvOverride, stub)
	a := o.New()
	if err := a.Prepare(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := a.Run(context.Background()); err == nil {
		t.Fatal("expected error for nonzero exit")
	}
}

func testTimeout(t *testing.T, o Options) {
	stub := writeStub(t, `sleep 5; echo "too late"`)
	t.Setenv(o.BinEnvOverride, stub)
	a := o.New()
	if err := a.Prepare(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{Goal: "hello", TimeoutSec: 1}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	start := time.Now()
	if _, err := a.Run(context.Background()); err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("timeout did not kill subprocess promptly: %v elapsed", elapsed)
	}
}
