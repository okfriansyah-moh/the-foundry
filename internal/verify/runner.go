package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// DefaultTimeout bounds a single command when Runner.Timeout is unset
// (docs/PLAN.md Task 13 Step 1: "10-min default timeout each").
const DefaultTimeout = 10 * time.Minute

// CommandRecord is one command Runner.Run attempted and its observed,
// evidence-grade outcome. StdoutDigest is a sha256 hex digest of the
// command's captured stdout, not the raw output, mirroring
// internal/evidence.CommandRecord so a kernel activity can copy these
// fields straight into an evidence.Manifest.
type CommandRecord struct {
	Cmd             string
	ExitCode        int
	StdoutDigest    string
	DurationMS      int64
	TimedOut        bool
	PolicyViolation bool
}

// Runner executes a task's commands the same, honest way every time: each
// command is tokenized (shlex-style, whitespace split) and exec'd
// argv-style with no shell, so shell metacharacters in a command string
// (";", backticks, "$(...)") are inert — they are passed as literal
// argv entries, never interpreted (docs/PLAN.md Task 13 Step 1).
type Runner struct {
	// Allowlist is checked against every command's first token before
	// anything runs (Step 2).
	Allowlist Allowlist
	// EnvAllowlist names the only environment variables visible to the
	// subprocess; all else is scrubbed.
	EnvAllowlist []string
	// Timeout bounds each command; DefaultTimeout is used when zero.
	Timeout time.Duration
}

// NewRunner builds a Runner with DefaultTimeout and no extra environment.
func NewRunner(allowlist Allowlist) Runner {
	return Runner{Allowlist: allowlist, Timeout: DefaultTimeout}
}

// Run executes cmds in order inside ws, cwd=ws.Path, and returns one
// CommandRecord per command attempted. It stops at the first command that
// violates the allowlist, times out, or exits nonzero — later validation
// commands generally assume earlier ones passed, so there is nothing
// useful to learn by continuing — and returns the records gathered so far
// with a nil error: those outcomes are the honest validation result, not a
// Runner fault. A non-nil error means Run itself could not run the
// commands at all (e.g. the binary was allowlisted but not installed),
// which is a genuine infrastructure fault, not a pass/fail verdict —
// callers should classify pass/fail from the returned records via
// Evaluate, never from this error.
func (r Runner) Run(ctx context.Context, ws worktree.Workspace, cmds []string) ([]CommandRecord, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	records := make([]CommandRecord, 0, len(cmds))
	for _, cmdLine := range cmds {
		argv := strings.Fields(cmdLine)
		if err := r.Allowlist.Check(argv); err != nil {
			records = append(records, CommandRecord{Cmd: cmdLine, ExitCode: -1, PolicyViolation: true})
			return records, nil
		}

		result, runErr := executor.RunSubprocess(ctx, ws.Path, cmdLine, r.EnvAllowlist, timeout)
		rec := CommandRecord{
			Cmd:          cmdLine,
			ExitCode:     result.ExitCode,
			StdoutDigest: digestHex(result.Stdout),
			DurationMS:   result.Duration.Milliseconds(),
			TimedOut:     result.TimedOut,
		}
		records = append(records, rec)

		if result.TimedOut {
			return records, nil
		}
		if runErr != nil {
			return records, fmt.Errorf("verify: run %q: %w", cmdLine, runErr)
		}
		if rec.ExitCode != 0 {
			return records, nil
		}
	}
	return records, nil
}

func digestHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
