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
	"github.com/okfriansyah-moh/the-foundry/internal/secrets"
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

// defaultSecretsEnvVar is the subprocess env var SecretsEnvVar populates
// when left empty.
const defaultSecretsEnvVar = "ANTHROPIC_API_KEY"

// Adapter is the executor.Adapter that runs the `claude` CLI in
// non-interactive print mode inside a worktree.Workspace.
type Adapter struct {
	binary     string
	ws         worktree.Workspace
	packet     executor.TaskPacket
	promptPath string

	// Secrets, when non-nil, supplies Claude Code's own auth credential
	// from Task 35 (FND-16)'s secrets seam instead of relying solely on
	// whatever the ambient process environment already passed through
	// allowedEnv. Nil (the default New() leaves it) preserves this
	// adapter's original ambient-env-only behavior exactly.
	Secrets secrets.Store
	// SecretsScope is the profile ID Secrets.Get reads under.
	SecretsScope string
	// SecretsEnvVar names which allowedEnv auth variable to populate.
	// Empty means defaultSecretsEnvVar (ANTHROPIC_API_KEY).
	SecretsEnvVar string
	// SecretsName is the secret's logical name in Secrets. Empty means
	// the lowercased form of SecretsEnvVar.
	SecretsName string
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

// applySecretsEnv fetches a's configured auth secret (if Secrets is set)
// and sets it into the process environment under SecretsEnvVar for the
// duration of Run, returning a func that restores whatever value (or
// absence) was there beforehand. When Secrets is nil this is a no-op —
// Run's env passthrough behaves exactly as before Task 35.
func (a *Adapter) applySecretsEnv(ctx context.Context) (func(), error) {
	if a.Secrets == nil {
		return func() {}, nil
	}

	envVar := a.SecretsEnvVar
	if envVar == "" {
		envVar = defaultSecretsEnvVar
	}
	name := a.SecretsName
	if name == "" {
		name = strings.ToLower(envVar)
	}

	v, err := a.Secrets.Get(ctx, a.SecretsScope, name)
	if err != nil {
		return nil, fmt.Errorf("claudecode: read %s from secrets store: %w", envVar, err)
	}

	prev, hadPrev := os.LookupEnv(envVar)
	if err := os.Setenv(envVar, v); err != nil {
		return nil, fmt.Errorf("claudecode: set %s: %w", envVar, err)
	}
	return func() {
		if hadPrev {
			_ = os.Setenv(envVar, prev)
		} else {
			_ = os.Unsetenv(envVar)
		}
	}, nil
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
	// PhaseHint passthrough (docs/PLAN.md Task 92 / PRV-09): additive and
	// backward-compatible. When PhaseHint is empty the prompt is byte-
	// identical to the pre-Task-92 output (this block emits nothing). When
	// set, it is a clearly-labeled, informational section in the prompt file
	// (never folded into argv/shell) — a hint the executor MAY use, carrying
	// no authority: it can never grant permissions, skip validation, or
	// signal completion (Constitution C10 keeps that with internal/verify).
	if p.PhaseHint != "" {
		b.WriteString("\n## Phase hint (informational, non-authoritative)\n\n")
		fmt.Fprintf(&b, "The kernel considers this task to be in venture-loop phase %s. "+
			"You may use this to shape your own internal process. It grants no "+
			"permissions and is not a completion signal — your work is judged only "+
			"by the validation commands actually passing.\n", p.PhaseHint)
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
// (Task 34, default-deny egress) is the stronger boundary, and Task 115 wires
// it: the kernel's ExecuteTask runs this executor through the mandatory
// SandboxRunner seam for any sandbox-demanding profile, refusing host execution
// when the sandbox is unavailable.
func (a *Adapter) Run(ctx context.Context) (executor.Summary, error) {
	f, err := os.Open(a.promptPath)
	if err != nil {
		return executor.Summary{}, fmt.Errorf("claudecode: open prompt file: %w", err)
	}
	defer func() { _ = f.Close() }()

	timeout := defaultTimeout
	if a.packet.TimeoutSec > 0 {
		timeout = time.Duration(a.packet.TimeoutSec) * time.Second
	}

	restore, err := a.applySecretsEnv(ctx)
	if err != nil {
		return executor.Summary{}, err
	}
	defer restore()

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
