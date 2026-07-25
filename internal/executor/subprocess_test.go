package executor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunSubprocess_EnvScrub(t *testing.T) {
	t.Setenv("EXEC_TEST_ALLOWED", "yes")
	t.Setenv("EXEC_TEST_SECRET", "leak-me-not")

	dir := t.TempDir()
	result, err := RunSubprocess(context.Background(), dir, "env", []string{"EXEC_TEST_ALLOWED"}, 5*time.Second)
	if err != nil {
		t.Fatalf("RunSubprocess: %v", err)
	}
	if !strings.Contains(result.Stdout, "EXEC_TEST_ALLOWED=yes") {
		t.Fatalf("allowlisted var missing from child env, stdout=%q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "EXEC_TEST_SECRET") {
		t.Fatalf("non-allowlisted var leaked into child env, stdout=%q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "PATH=") {
		t.Fatalf("non-allowlisted PATH leaked into child env, stdout=%q", result.Stdout)
	}
}

func TestRunSubprocess_ArgvNoShellExpansion(t *testing.T) {
	dir := t.TempDir()
	// A shell would treat ";" as a command separator and run "rm -rf nope".
	// RunSubprocess never invokes a shell, so this must print the literal
	// tokens and must not touch the filesystem.
	result, err := RunSubprocess(context.Background(), dir, "echo hello;rm -rf nope", nil, 5*time.Second)
	if err != nil {
		t.Fatalf("RunSubprocess: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello;rm") {
		t.Fatalf("expected literal token in output, got %q", result.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "nope")); !os.IsNotExist(err) {
		t.Fatalf("shell metacharacter was interpreted: nope exists (err=%v)", err)
	}
}

func TestRunSubprocess_TimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	scriptPath, err := filepath.Abs("testdata/spawn_child.sh")
	if err != nil {
		t.Fatalf("resolve script path: %v", err)
	}

	cmdLine := "sh " + scriptPath + " " + pidFile
	_, err = RunSubprocess(context.Background(), dir, cmdLine, []string{"PATH"}, 300*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}

	// Signal 0 checks liveness without actually signaling the process.
	if sigErr := syscall.Kill(pid, syscall.Signal(0)); sigErr == nil {
		t.Fatalf("grandchild pid %d still alive after process-group kill", pid)
	}
}
