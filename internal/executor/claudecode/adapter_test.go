package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// writeStub writes an executable shell script at dir/name and returns its
// absolute path. Used to stand in for the real `claude` binary so unit
// tests never depend on network access, credentials, or API spend.
func writeStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func newWorkspace(t *testing.T) worktree.Workspace {
	t.Helper()
	return worktree.Workspace{Path: t.TempDir()}
}

func TestRegistered(t *testing.T) {
	a, err := executor.Get("claude-code")
	if err != nil {
		t.Fatalf("executor.Get(claude-code): %v", err)
	}
	if _, ok := a.(*Adapter); !ok {
		t.Fatalf("registered adapter is %T, want *Adapter", a)
	}
}

func TestPrepare_WritesPromptFileInsideWorkspaceOnly(t *testing.T) {
	ws := newWorkspace(t)
	a := New()
	packet := executor.TaskPacket{
		PlanID:             "plan-1",
		TaskID:             "../../etc/passwd", // must not influence the path at all
		Goal:               "say hello",
		Commands:           []string{"echo hi"},
		ValidationCommands: []string{"true"},
	}
	if err := a.Prepare(context.Background(), ws, packet); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	wantPath := filepath.Join(ws.Path, promptFileName)
	if a.promptPath != wantPath {
		t.Fatalf("promptPath = %q, want %q (packet.TaskID must not affect the path)", a.promptPath, wantPath)
	}
	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("prompt file missing: %v", err)
	}
	content := string(b)
	for _, want := range []string{"plan-1", "say hello", "echo hi", "true"} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt content missing %q, got: %s", want, content)
		}
	}
}

func TestPrepare_RejectsEmptyWorkspaceOrGoal(t *testing.T) {
	a := New()
	if err := a.Prepare(context.Background(), worktree.Workspace{}, executor.TaskPacket{Goal: "x"}); err == nil {
		t.Fatalf("expected error for empty workspace path")
	}
	if err := a.Prepare(context.Background(), newWorkspace(t), executor.TaskPacket{}); err == nil {
		t.Fatalf("expected error for empty goal")
	}
}

func TestRun_NoSecretLeak(t *testing.T) {
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "env_dump.txt")
	stub := writeStub(t, dir, "claude", `env > `+dumpPath+`
cat <<'EOF'
{"result":"ok","session_id":"s1","num_turns":1,"duration_ms":10,"total_cost_usd":0.01,"usage":{"input_tokens":1}}
EOF
`)

	t.Setenv(binaryEnvOverride, stub)
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-be-visible")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_leak_me_not")
	t.Setenv("GITHUB_TOKEN", "ghp_leak_me_not")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak_me_not")
	t.Setenv("DATABASE_URL", "postgres://leak_me_not")

	ws := newWorkspace(t)
	a := New()
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	dump, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	got := string(dump)

	for _, secret := range []string{"STRIPE_SECRET_KEY", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "DATABASE_URL", "leak_me_not"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret-like var leaked into claude subprocess env: %q found in dump:\n%s", secret, got)
		}
	}
	if !strings.Contains(got, "ANTHROPIC_API_KEY=sk-should-be-visible") {
		t.Fatalf("required auth var missing from subprocess env, dump:\n%s", got)
	}
}

func TestRun_ParsesCostAndTokenTelemetry(t *testing.T) {
	dir := t.TempDir()
	stub := writeStub(t, dir, "claude", `cat <<'EOF'
{"result":"hello world","session_id":"abc123","num_turns":3,"duration_ms":4200,"total_cost_usd":0.0456,"usage":{"input_tokens":100,"output_tokens":20}}
EOF
`)
	t.Setenv(binaryEnvOverride, stub)

	ws := newWorkspace(t)
	a := New()
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Claimed != "hello world" {
		t.Fatalf("Claimed = %q, want %q", summary.Claimed, "hello world")
	}
	for _, want := range []string{"session_id=abc123", "num_turns=3", "duration_ms=4200", "total_cost_usd=0.0456", "input_tokens"} {
		if !strings.Contains(summary.ExitNotes, want) {
			t.Fatalf("ExitNotes missing %q, got: %s", want, summary.ExitNotes)
		}
	}
}

func TestRun_NonJSONStdoutFallsBackGracefully(t *testing.T) {
	dir := t.TempDir()
	stub := writeStub(t, dir, "claude", `echo "not json"`)
	t.Setenv(binaryEnvOverride, stub)

	ws := newWorkspace(t)
	a := New()
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Claimed != "not json" {
		t.Fatalf("Claimed = %q, want fallback raw stdout", summary.Claimed)
	}
}

func TestRun_NonzeroExitIsError(t *testing.T) {
	dir := t.TempDir()
	stub := writeStub(t, dir, "claude", `echo '{"result":"boom"}'; exit 1`)
	t.Setenv(binaryEnvOverride, stub)

	ws := newWorkspace(t)
	a := New()
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := a.Run(context.Background()); err == nil {
		t.Fatalf("expected error for nonzero exit")
	}
}

func TestRun_TimeoutKillsSubprocess(t *testing.T) {
	dir := t.TempDir()
	stub := writeStub(t, dir, "claude", `sleep 5; echo '{"result":"too late"}'`)
	t.Setenv(binaryEnvOverride, stub)

	ws := newWorkspace(t)
	a := New()
	packet := executor.TaskPacket{Goal: "hello", TimeoutSec: 1}
	if err := a.Prepare(context.Background(), ws, packet); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	start := time.Now()
	if _, err := a.Run(context.Background()); err == nil {
		t.Fatalf("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("Run took %s, expected to be killed near TimeoutSec=1s", elapsed)
	}
}

func TestCollect_ReturnsPromptFile(t *testing.T) {
	ws := newWorkspace(t)
	a := New()
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	artifacts, err := a.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(artifacts.Paths) != 1 || artifacts.Paths[0] != promptFileName {
		t.Fatalf("Collect() = %v, want [%s]", artifacts.Paths, promptFileName)
	}
}
