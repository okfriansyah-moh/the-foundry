package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func init() {
	executor.Register("claude-code", func() executor.Adapter { return New() })
}

// defaultBinary is the executable name looked up on PATH.
const defaultBinary = "claude"

// binaryEnvOverride lets tests point Adapter at a stub binary instead of a
// real `claude` install, without touching production wiring.
const binaryEnvOverride = "FOUNDRY_CLAUDE_CODE_BIN"

// promptFileName is the fixed name of the prompt file written inside the
// workspace. It is never derived from TaskPacket fields, so nothing in the
// packet (PlanID, TaskID, Goal) can influence the resulting path — this
// closes off path traversal via packet content by construction.
const promptFileName = ".foundry-claude-code-prompt.md"

// allowedEnv is the exhaustive set of environment variables ever visible to
// the `claude` subprocess. This adapter deliberately does not honor
// TaskPacket.EnvAllowlist — the task card requires excluding every secret
// except what Claude Code's own auth needs, and that requirement must hold
// regardless of what a caller passes in, so it is enforced here as a fixed
// allowlist rather than a caller-widenable one (docs/PLAN.md Task 17
// Acceptance: "no secret appears in workspace env dump").
var allowedEnv = []string{
	"PATH",                    // resolve tools the agent invokes (git, node, go, ...)
	"HOME",                    // locate Claude Code's config/credentials directory
	"CLAUDE_CONFIG_DIR",       // optional override of the above
	"ANTHROPIC_API_KEY",       // API-key auth path
	"CLAUDE_CODE_OAUTH_TOKEN", // long-lived token auth path (claude setup-token)
}

// defaultTimeout applies when TaskPacket.TimeoutSec is unset.
const defaultTimeout = 30 * time.Minute

// Adapter is the executor.Adapter that runs the `claude` CLI in
// non-interactive print mode inside a worktree.Workspace.
type Adapter struct {
	binary     string
	ws         worktree.Workspace
	packet     executor.TaskPacket
	promptPath string
}

// New constructs a fresh Adapter. The binary is "claude" unless
// FOUNDRY_CLAUDE_CODE_BIN overrides it (test seam only).
func New() *Adapter {
	bin := defaultBinary
	if v := os.Getenv(binaryEnvOverride); v != "" {
		bin = v
	}
	return &Adapter{binary: bin}
}

// Prepare writes the task packet's content to a fixed-name prompt file
// inside ws.Path. It does not touch anything outside ws.Path.
func (a *Adapter) Prepare(_ context.Context, ws worktree.Workspace, packet executor.TaskPacket) error {
	if ws.Path == "" {
		return fmt.Errorf("claudecode: workspace path is empty")
	}
	if packet.Goal == "" {
		return fmt.Errorf("claudecode: packet.Goal must describe the task")
	}

	a.ws = ws
	a.packet = packet
	a.promptPath = filepath.Join(ws.Path, promptFileName)

	if err := os.WriteFile(a.promptPath, []byte(renderPrompt(packet)), 0o600); err != nil {
		return fmt.Errorf("claudecode: write prompt file: %w", err)
	}
	return nil
}

// renderPrompt turns a TaskPacket into the prompt text handed to Claude
// Code. The packet is kernel/PEC-approved task content, not arbitrary
// external text — it is still presented as a clearly delimited task
// description rather than raw instructions, consistent with LLM01
// (prompt-injection) hygiene, since Foundry's authority model (C4) never
// lets the executor's own output trigger a side effect directly regardless
// of what this prompt says.
func renderPrompt(p executor.TaskPacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Foundry task %s / %s\n\n", p.PlanID, p.TaskID)
	b.WriteString("## Goal\n\n")
	b.WriteString(p.Goal)
	b.WriteString("\n\n")
	if len(p.Commands) > 0 {
		b.WriteString("## Commands to run\n\n")
		for _, c := range p.Commands {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}
	if len(p.ValidationCommands) > 0 {
		b.WriteString("## Validation commands (must pass)\n\n")
		for _, c := range p.ValidationCommands {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	return b.String()
}

// Run invokes `claude -p --output-format json --permission-mode
// bypassPermissions`, feeding the prompt file's contents on stdin (not as
// an argv token or shell string) and capped by packet.TimeoutSec.
//
// --permission-mode bypassPermissions is required for unattended print-mode
// execution — without it, any tool-use permission prompt has no TTY to
// answer it and the run stalls. This is safe only because Prepare has
// already confined cmd.Dir to the isolated worktree (C8) and allowedEnv
// strips every credential except Claude Code's own; the executor sandbox
// (Task 34, default-deny egress) is the intended stronger boundary once it
// exists.
func (a *Adapter) Run(ctx context.Context) (executor.Summary, error) {
	f, err := os.Open(a.promptPath)
	if err != nil {
		return executor.Summary{}, fmt.Errorf("claudecode: open prompt file: %w", err)
	}
	defer f.Close()

	timeout := defaultTimeout
	if a.packet.TimeoutSec > 0 {
		timeout = time.Duration(a.packet.TimeoutSec) * time.Second
	}

	cmdLine := a.binary + " -p --output-format json --permission-mode bypassPermissions"
	result, err := executor.RunSubprocessWithStdin(ctx, a.ws.Path, cmdLine, f, allowedEnv, timeout)
	if err != nil {
		return executor.Summary{}, fmt.Errorf("claudecode: run: %w", err)
	}

	summary := parseSummary(result.Stdout)
	if result.ExitCode != 0 {
		return summary, fmt.Errorf("claudecode: claude exited %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return summary, nil
}

// cliResult is the subset of `claude --print --output-format json`'s
// single-result JSON object this adapter reads. Field names are per the
// installed CLI's documented behavior at implementation time (see
// docs/notes/claude-code-flags.md) — parsing is best-effort: an unexpected
// or evolved schema falls back to raw stdout rather than failing the run,
// since Summary is untrusted telemetry, not a contract.
type cliResult struct {
	Result       string          `json:"result"`
	IsError      bool            `json:"is_error"`
	SessionID    string          `json:"session_id"`
	NumTurns     int             `json:"num_turns"`
	DurationMS   int64           `json:"duration_ms"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	Usage        json.RawMessage `json:"usage"`
}

// parseSummary builds a Summary from stdout, capturing cost/token telemetry
// into ExitNotes when present.
func parseSummary(stdout string) executor.Summary {
	var r cliResult
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		return executor.Summary{
			Claimed:   strings.TrimSpace(stdout),
			ExitNotes: "claude-code: --output-format json did not parse; raw stdout captured as Claimed",
		}
	}
	notes := fmt.Sprintf(
		"session_id=%s num_turns=%d duration_ms=%d total_cost_usd=%.4f usage=%s",
		r.SessionID, r.NumTurns, r.DurationMS, r.TotalCostUSD, string(r.Usage),
	)
	return executor.Summary{Claimed: r.Result, ExitNotes: notes}
}

// Collect reports the prompt file as the only artifact this adapter itself
// wrote; anything Claude Code changed in the workspace is picked up by the
// kernel's own evidence collection (Task 13), not duplicated here.
func (a *Adapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{Paths: []string{promptFileName}}, nil
}
