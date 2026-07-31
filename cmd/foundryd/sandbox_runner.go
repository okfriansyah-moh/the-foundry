package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/sandbox"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// docs/PLAN.md Task 115 (SEC-01): the production SandboxRunner. It wraps
// internal/executor/sandbox.Runner (engine + gate + egress allowlist + rootless
// default) and is the ONLY place autonomous executor code runs. It lives in
// cmd/foundryd (not internal/kernel) so the kernel package keeps its
// bare-subprocess ban (the `fitlint subprocess` rule) — the kernel calls this
// through the kernel.SandboxRunner seam, never exec directly.
type ociSandboxRunner struct {
	engine            string
	image             string
	allowlist         sandbox.EgressAllowlist
	allowlistHostPath string
}

// newOCISandboxRunner loads the egress allowlist and returns a runner, or an
// error that makes the sandbox unavailable (fail-closed at Preflight).
func newOCISandboxRunner() (*ociSandboxRunner, error) {
	allowlistPath := envOr("FOUNDRY_SANDBOX_EGRESS_ALLOWLIST", "config/sandbox-egress-allowlist.yaml")
	allow, err := sandbox.LoadEgressAllowlist(allowlistPath)
	if err != nil {
		return nil, fmt.Errorf("load sandbox egress allowlist: %w", err)
	}
	return &ociSandboxRunner{
		engine:            envOr("FOUNDRY_SANDBOX_ENGINE", ""), // "" => sandbox default (podman)
		image:             envOr("FOUNDRY_SANDBOX_IMAGE", sandbox.DefaultImage),
		allowlist:         allow,
		allowlistHostPath: allowlistPath,
	}, nil
}

// Preflight verifies the container engine binary is present. A missing engine
// makes ExecuteTask refuse a sandbox-required task rather than run it on the
// host (C24).
func (r *ociSandboxRunner) Preflight(_ context.Context) error {
	engine := r.engine
	if engine == "" {
		engine = "podman"
	}
	if _, err := exec.LookPath(engine); err != nil {
		return fmt.Errorf("sandbox engine %q not found: %w", engine, err)
	}
	if len(r.allowlist.Allow) == 0 {
		return fmt.Errorf("sandbox egress allowlist is empty")
	}
	return nil
}

// RunSpec runs spec inside a fresh sandbox bound to workspacePath.
func (r *ociSandboxRunner) RunSpec(ctx context.Context, workspacePath string, spec executor.SandboxSpec) (executor.CommandResult, error) {
	// The OCI runner has no stdin plumbing. SandboxSpec.Stdin promises stdin
	// is written, so silently ignoring it would run a different command than
	// the adapter specified. Fail closed rather than degrade silently (C24:
	// sandbox-required work never falls back to altered behavior).
	if len(spec.Stdin) > 0 {
		return executor.CommandResult{}, fmt.Errorf("sandbox runner: SandboxSpec.Stdin is set but the OCI runner does not support stdin")
	}
	cfg := sandbox.Config{
		Engine:            r.engine,
		Image:             r.image,
		WorkspaceHost:     workspacePath,
		Allowlist:         r.allowlist,
		AllowlistHostPath: r.allowlistHostPath,
		EnvAllowlist:      spec.EnvAllowlist,
		CacheMounts:       sandbox.DefaultCacheMounts(),
		Timeout:           spec.Timeout,
	}
	runner, err := sandbox.NewRunner(cfg)
	if err != nil {
		return executor.CommandResult{}, fmt.Errorf("construct sandbox runner: %w", err)
	}
	if err := runner.Start(ctx); err != nil {
		return executor.CommandResult{}, fmt.Errorf("start sandbox: %w", err)
	}
	defer func() { _ = runner.Close(ctx) }()
	return runner.RunCommand(ctx, spec.Argv, spec.Timeout)
}

// wireSandbox attaches the production sandbox runner to the kernel activities
// unless explicitly disabled (FOUNDRY_SANDBOX_DISABLED=1, dev only). A wiring
// failure is logged and leaves Sandbox nil, which makes a sandbox-required task
// refuse — never a silent host fallback.
func wireSandbox(a *kernel.Activities) {
	if os.Getenv("FOUNDRY_SANDBOX_DISABLED") == "1" {
		return
	}
	runner, err := newOCISandboxRunner()
	if err != nil {
		fmt.Fprintf(os.Stderr, "foundryd: sandbox runner unavailable (sandbox-required tasks will refuse): %v\n", err)
		return
	}
	a.Sandbox = runner
}
