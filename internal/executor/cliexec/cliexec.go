package cliexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// Config declares one CLI-executor provider's fixed, package-confined
// invocation details. Every field is set by the concrete provider package;
// this package holds no provider defaults of its own beyond DefaultTimeout.
type Config struct {
	// Provider is the executor registry name (e.g. "opencode"), used in
	// error messages and as the default artifact/prompt-file basis.
	Provider string
	// Binary is the executable name looked up on PATH.
	Binary string
	// BinEnvOverride, when non-empty, names an environment variable a test
	// may set to point Binary at a stub executable — mirrors Task 17's
	// FOUNDRY_CLAUDE_CODE_BIN. Never used in production wiring.
	BinEnvOverride string
	// Args are the fixed arguments appended after Binary to force
	// non-interactive/headless execution. They never contain packet data —
	// the prompt is fed on stdin, keeping cmdLine a constant argv.
	Args []string
	// AllowedEnv is the EXHAUSTIVE set of environment variables ever visible
	// to the subprocess. It is defined by the provider package and is NOT
	// TaskPacket.EnvAllowlist — a caller can never widen it, so no secret
	// outside this fixed set can leak into the provider CLI.
	AllowedEnv []string
	// PromptFile is the fixed filename the rendered prompt is written to
	// inside the workspace. Never derived from packet fields.
	PromptFile string
	// DefaultTimeout applies when TaskPacket.TimeoutSec is unset.
	DefaultTimeout time.Duration
	// ParseSummary converts the CLI's stdout into an executor.Summary. It
	// must not leak provider-specific field names into Summary. When nil,
	// RawSummary (trimmed stdout as Claimed) is used.
	ParseSummary func(stdout string) executor.Summary
}

// Adapter is a generic executor.Adapter for a headless, stdin-driven CLI
// provider. All state is per-instance; New returns a fresh one each call.
type Adapter struct {
	cfg        Config
	binary     string
	ws         worktree.Workspace
	packet     executor.TaskPacket
	promptPath string
}

// New constructs a fresh Adapter for cfg, honoring cfg.BinEnvOverride (test
// seam) if set.
func New(cfg Config) *Adapter {
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.ParseSummary == nil {
		cfg.ParseSummary = RawSummary
	}
	bin := cfg.Binary
	if cfg.BinEnvOverride != "" {
		if v := os.Getenv(cfg.BinEnvOverride); v != "" {
			bin = v
		}
	}
	return &Adapter{cfg: cfg, binary: bin}
}

// Prepare writes the packet's rendered prompt to a fixed-name file inside
// ws.Path. It touches nothing outside ws.Path.
func (a *Adapter) Prepare(_ context.Context, ws worktree.Workspace, packet executor.TaskPacket) error {
	if ws.Path == "" {
		return fmt.Errorf("%s: workspace path is empty", a.cfg.Provider)
	}
	if packet.Goal == "" {
		return fmt.Errorf("%s: packet.Goal must describe the task", a.cfg.Provider)
	}
	a.ws = ws
	a.packet = packet
	a.promptPath = filepath.Join(ws.Path, a.cfg.PromptFile)
	if err := os.WriteFile(a.promptPath, []byte(RenderPrompt(packet)), 0o600); err != nil {
		return fmt.Errorf("%s: write prompt file: %w", a.cfg.Provider, err)
	}
	return nil
}

// Run invokes the provider CLI headlessly, feeding the prompt file on stdin.
func (a *Adapter) Run(ctx context.Context) (executor.Summary, error) {
	if a.promptPath == "" {
		return executor.Summary{}, fmt.Errorf("%s: Run called before Prepare", a.cfg.Provider)
	}
	f, err := os.Open(a.promptPath)
	if err != nil {
		return executor.Summary{}, fmt.Errorf("%s: open prompt file: %w", a.cfg.Provider, err)
	}
	defer func() { _ = f.Close() }()

	timeout := a.cfg.DefaultTimeout
	if a.packet.TimeoutSec > 0 {
		timeout = time.Duration(a.packet.TimeoutSec) * time.Second
	}

	cmdLine := a.binary
	if len(a.cfg.Args) > 0 {
		cmdLine = a.binary + " " + strings.Join(a.cfg.Args, " ")
	}
	result, err := executor.RunSubprocessWithStdin(ctx, a.ws.Path, cmdLine, f, a.cfg.AllowedEnv, timeout)
	if err != nil {
		return executor.Summary{}, fmt.Errorf("%s: run: %w", a.cfg.Provider, err)
	}

	summary := a.cfg.ParseSummary(result.Stdout)
	if result.ExitCode != 0 {
		return summary, fmt.Errorf("%s: %s exited %d: %s", a.cfg.Provider, a.cfg.Provider, result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return summary, nil
}

// Collect reports the prompt file as this adapter's only written artifact;
// anything the CLI changed in the workspace is picked up by the kernel's own
// evidence collection (Task 13), not duplicated here.
func (a *Adapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{Paths: []string{a.cfg.PromptFile}}, nil
}

// SandboxSpec implements executor.SandboxSpecProvider (docs/PLAN.md Task 142).
func (a *Adapter) SandboxSpec(_ context.Context, ws worktree.Workspace, packet executor.TaskPacket) (executor.SandboxSpec, error) {
	bin := a.binary
	if bin == "" {
		bin = a.cfg.Binary
	}
	timeout := a.cfg.DefaultTimeout
	if packet.TimeoutSec > 0 {
		timeout = time.Duration(packet.TimeoutSec) * time.Second
	}
	argv := []string{bin}
	argv = append(argv, a.cfg.Args...)
	var stdin []byte
	if a.promptPath != "" {
		if raw, err := os.ReadFile(a.promptPath); err == nil {
			stdin = raw
		}
	}
	return executor.SandboxSpec{
		Executable:    bin,
		Argv:          argv,
		Stdin:         stdin,
		EnvAllowlist:  append([]string(nil), a.cfg.AllowedEnv...),
		WorkingDir:    ws.Path,
		Timeout:       timeout,
		ArtifactPaths: []string{a.cfg.PromptFile},
	}, nil
}

// RenderPrompt turns a TaskPacket into the delimited prompt text handed to a
// CLI provider. Identical in shape to Task 17's claude-code prompt: the
// packet is kernel/PEC-approved task content, presented as a clearly
// delimited task description (LLM01 hygiene) — the authority model (C4)
// never lets an executor's output trigger a side effect regardless of what
// this prompt says.
func RenderPrompt(p executor.TaskPacket) string {
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

// RawSummary is the default ParseSummary: it captures trimmed stdout as the
// (untrusted) Claimed outcome and leaks no provider-specific field names.
func RawSummary(stdout string) executor.Summary {
	return executor.Summary{Claimed: strings.TrimSpace(stdout)}
}
