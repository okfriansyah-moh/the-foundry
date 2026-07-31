package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// CommandResult is the observed outcome of running one command line.
type CommandResult struct {
	Cmd      string
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	TimedOut bool
}

// RunSubprocess tokenizes cmdLine into an argv slice (whitespace-separated;
// no shell is ever invoked, so shell metacharacters have no special
// meaning) and runs it with:
//   - working directory dir,
//   - an environment scrubbed down to only the vars named in envAllowlist,
//   - its own process group (Setpgid), so that if timeout elapses or ctx is
//     canceled the entire process group — not just the direct child — is
//     killed, leaving no orphans.
//
// A zero or negative timeout means no deadline beyond ctx's own.
func RunSubprocess(ctx context.Context, dir, cmdLine string, envAllowlist []string, timeout time.Duration) (CommandResult, error) {
	return runSubprocess(ctx, dir, cmdLine, nil, envAllowlist, nil, timeout)
}

// RunSubprocessWithStdin behaves exactly like RunSubprocess but additionally
// wires stdin to the child process. This exists for adapters that must feed
// large or free-form content (e.g. a prompt file's contents) to a
// subprocess without folding that content into cmdLine — cmdLine stays a
// fixed, argv-tokenized command with no interpolated data, avoiding any
// argv-injection or shell-quoting concern for the piped content. A nil
// stdin behaves identically to RunSubprocess.
func RunSubprocessWithStdin(ctx context.Context, dir, cmdLine string, stdin io.Reader, envAllowlist []string, timeout time.Duration) (CommandResult, error) {
	return runSubprocess(ctx, dir, cmdLine, stdin, envAllowlist, nil, timeout)
}

// RunSubprocessWithEnv is the concurrency-safe credential-passing entry point
// (docs/PLAN.md Task 117 / SEC-03). extraEnv is injected into ONLY this child
// process's environment (overriding any allowlisted value of the same name); it
// is NEVER written to the shared daemon process environment via os.Setenv, so
// two concurrent tasks in different secret scopes can never observe each other's
// credential. A nil extraEnv behaves identically to RunSubprocessWithStdin.
func RunSubprocessWithEnv(ctx context.Context, dir, cmdLine string, stdin io.Reader, envAllowlist []string, extraEnv map[string]string, timeout time.Duration) (CommandResult, error) {
	return runSubprocess(ctx, dir, cmdLine, stdin, envAllowlist, extraEnv, timeout)
}

func runSubprocess(ctx context.Context, dir, cmdLine string, stdin io.Reader, envAllowlist []string, extraEnv map[string]string, timeout time.Duration) (CommandResult, error) {
	argv := strings.Fields(cmdLine)
	if len(argv) == 0 {
		return CommandResult{}, fmt.Errorf("executor: empty command")
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// #nosec G204 -- argv is a tokenized slice passed directly to
	// exec.Command, never a shell string; callers control cmdLine content
	// via TaskPacket, which is documented as untrusted-ish input.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = buildChildEnv(envAllowlist, extraEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = stdin

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return CommandResult{}, fmt.Errorf("executor: start %q: %w", cmdLine, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-runCtx.Done():
		timedOut = true
		killProcessGroup(cmd.Process.Pid)
		<-done // reap; ignore its error, the process was killed on purpose
	}

	result := CommandResult{
		Cmd:      cmdLine,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
		TimedOut: timedOut,
	}
	if timedOut {
		result.ExitCode = -1
		return result, fmt.Errorf("executor: %q: %w", cmdLine, context.DeadlineExceeded)
	}
	result.ExitCode = cmd.ProcessState.ExitCode()
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return result, fmt.Errorf("executor: run %q: %w", cmdLine, waitErr)
		}
	}
	return result, nil
}

// killProcessGroup sends SIGKILL to the entire process group led by pid, so
// that children the command itself spawned die with it instead of being
// orphaned.
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// scrubEnv returns an environment containing only the variables named in
// allowlist, read from the current process's environment. Variables not
// named are never visible to the subprocess.
func scrubEnv(allowlist []string) []string {
	env := make([]string, 0, len(allowlist))
	for _, name := range allowlist {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// buildChildEnv constructs the child process environment from the allowlisted
// host variables plus per-invocation extraEnv (Task 117). extraEnv is applied
// to the constructed child env only — never to the parent — and overrides any
// allowlisted value of the same name. This is the concurrency-safe replacement
// for os.Setenv on the shared daemon process.
func buildChildEnv(allowlist []string, extraEnv map[string]string) []string {
	base := scrubEnv(allowlist)
	if len(extraEnv) == 0 {
		return base
	}
	// Drop any allowlisted entry that extraEnv overrides, then append extraEnv.
	out := make([]string, 0, len(base)+len(extraEnv))
	for _, kv := range base {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if _, overridden := extraEnv[name]; overridden {
			continue
		}
		out = append(out, kv)
	}
	// Deterministic order for reproducibility.
	names := make([]string, 0, len(extraEnv))
	for name := range extraEnv {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, name+"="+extraEnv[name])
	}
	return out
}
