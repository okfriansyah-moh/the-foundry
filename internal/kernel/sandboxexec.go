package kernel

import (
	"context"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// docs/PLAN.md Task 115 (SEC-01): the mandatory-sandbox execution seam. When a
// task's profile policy demands sandboxing, ExecuteTask runs the resolved
// executor INSIDE the sandbox and refuses to execute at all when the sandbox is
// unavailable — it never falls back to host execution (Constitution C4/C24).

// Named refusal classifications for the sandbox path. They are asserted by the
// escape suite through ExecuteTask and are stable strings, not log text.
const (
	// ClassificationSandboxUnavailable is returned when a sandbox-required
	// task cannot be sandboxed: no runner wired, the engine/image/gate is
	// unavailable, or the allowlist failed to load.
	ClassificationSandboxUnavailable = "sandbox-unavailable"
	// ClassificationSandboxIncompatible is returned when a sandbox-required
	// executor's adapter cannot express its work as a SandboxSpec, so it
	// cannot be run inside the sandbox.
	ClassificationSandboxIncompatible = "sandbox-incompatible"
	// ClassificationSandboxOptOutRefused is returned when a profile demands
	// sandboxing but the resolved executor declared requires_sandbox=false.
	ClassificationSandboxOptOutRefused = "sandbox-optout-refused"
)

// SandboxRunner is the kernel-owned seam over internal/executor/sandbox. The
// production implementation wraps sandbox.Runner (engine + gate + egress
// allowlist + rootless default); tests inject a fake. It is deliberately
// minimal so the kernel never learns container mechanics.
type SandboxRunner interface {
	// Preflight returns nil only when the sandbox engine, image and gate are
	// usable. A non-nil error means execution must be refused — there is no
	// host fallback (C24).
	Preflight(ctx context.Context) error
	// RunSpec executes spec inside a fresh sandbox bound to workspacePath and
	// returns the command result.
	RunSpec(ctx context.Context, workspacePath string, spec executor.SandboxSpec) (executor.CommandResult, error)
}

// sandboxDecision is the outcome of deciding how a task must execute.
type sandboxDecision struct {
	// required is whether this task must run sandboxed (profile demands it and
	// the executor requires it).
	required bool
	// refusal, when non-empty, is a named classification the task must be
	// refused with — no execution of any kind may proceed.
	refusal string
	// reason accompanies refusal for the error message.
	reason string
}

// decideSandbox resolves whether execName must be sandboxed for this task and,
// if it cannot be, why it must be refused. requireSandbox comes from the task's
// resolved profile policy (ExecuteTaskInput.RequireSandbox).
func (a *Activities) decideSandbox(execName string, requireSandbox bool) sandboxDecision {
	if !requireSandbox {
		// The profile does not demand sandboxing for this task; legacy path.
		return sandboxDecision{required: false}
	}
	// The profile demands sandboxing. The executor's capability record refines
	// it: an executor that opted out (requires_sandbox=false) is refused for a
	// sandbox-demanding profile.
	if rec, ok := a.CapabilityRegistry.Lookup(execName); ok && !rec.SandboxRequired() {
		return sandboxDecision{
			refusal: ClassificationSandboxOptOutRefused,
			reason:  fmt.Sprintf("executor %q opted out of the sandbox (%s) but this profile demands it", execName, rec.SandboxOptOutReason),
		}
	}
	return sandboxDecision{required: true}
}

// runSandboxed executes the adapter inside the sandbox, fail-closed. It returns
// the executor summary on success, or an ExecuteTaskOutput carrying a named
// refusal classification (and ok=false) when the task must not run at all.
func (a *Activities) runSandboxed(ctx context.Context, execName string, adapter executor.Adapter, ws worktree.Workspace, packet executor.TaskPacket) (executor.Summary, ExecuteTaskOutput, bool) {
	// 1. A sandbox runner must be wired. Its absence is a refusal, never a host
	//    fallback.
	if a.Sandbox == nil {
		return executor.Summary{}, refuseSandbox(execName, ClassificationSandboxUnavailable,
			"no sandbox runner wired: sandbox-required task refuses to run on the host"), false
	}
	// 2. The engine, image and gate must be usable right now.
	if err := a.Sandbox.Preflight(ctx); err != nil {
		return executor.Summary{}, refuseSandbox(execName, ClassificationSandboxUnavailable,
			fmt.Sprintf("sandbox unavailable: %v", err)), false
	}
	// 3. The adapter must be able to express its work as a SandboxSpec.
	provider, ok := adapter.(executor.SandboxSpecProvider)
	if !ok {
		return executor.Summary{}, refuseSandbox(execName, ClassificationSandboxIncompatible,
			fmt.Sprintf("executor %q cannot be run inside the sandbox (no SandboxSpec)", execName)), false
	}
	spec, err := provider.SandboxSpec(ctx, ws, packet)
	if err != nil {
		return executor.Summary{}, refuseSandbox(execName, ClassificationSandboxIncompatible,
			fmt.Sprintf("executor %q sandbox spec: %v", execName, err)), false
	}
	// 4. Run the command inside the sandbox.
	res, err := a.Sandbox.RunSpec(ctx, ws.Path, spec)
	if err != nil {
		// A sandbox execution error is a task failure, not a host fallback.
		return executor.Summary{}, ExecuteTaskOutput{
			Failed: true, ExecutorUsed: execName,
			ErrorMessage: fmt.Sprintf("sandbox run failed: %v", err),
		}, false
	}
	summary := executor.Summary{
		Claimed:   fmt.Sprintf("sandboxed exit %d", res.ExitCode),
		ExitNotes: res.Stdout,
	}
	if res.ExitCode != 0 || res.TimedOut {
		return summary, ExecuteTaskOutput{
			Failed: true, ExecutorUsed: execName,
			ErrorMessage: fmt.Sprintf("sandboxed command failed: exit %d (timed_out=%v): %s", res.ExitCode, res.TimedOut, res.Stderr),
			Claimed:      summary.Claimed, ExitNotes: summary.ExitNotes,
		}, false
	}
	return summary, ExecuteTaskOutput{}, true
}

// refuse builds a fail-closed ExecuteTaskOutput with a named classification.
func refuseSandbox(execName, classification, reason string) ExecuteTaskOutput {
	return ExecuteTaskOutput{
		Failed:         true,
		ExecutorUsed:   execName,
		Classification: classification,
		ErrorMessage:   reason,
	}
}
