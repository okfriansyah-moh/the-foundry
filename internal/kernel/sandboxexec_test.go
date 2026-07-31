package kernel

import (
	"context"
	"errors"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// sandboxCapableAdapter is a fake executor that CAN be sandboxed. It records
// whether Run was ever called so tests can prove the sandbox path never invokes
// the host adapter.Run.
type sandboxCapableAdapter struct{ ranOnHost bool }

func (a *sandboxCapableAdapter) Prepare(context.Context, worktree.Workspace, executor.TaskPacket) error {
	return nil
}
func (a *sandboxCapableAdapter) Run(context.Context) (executor.Summary, error) {
	a.ranOnHost = true
	return executor.Summary{Claimed: "ran on host"}, nil
}
func (a *sandboxCapableAdapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{}, nil
}
func (a *sandboxCapableAdapter) SandboxSpec(context.Context, worktree.Workspace, executor.TaskPacket) (executor.SandboxSpec, error) {
	return executor.SandboxSpec{Argv: []string{"echo", "hi"}}, nil
}

// plainAdapter cannot be sandboxed (no SandboxSpec).
type plainAdapter struct{ ranOnHost bool }

func (a *plainAdapter) Prepare(context.Context, worktree.Workspace, executor.TaskPacket) error {
	return nil
}
func (a *plainAdapter) Run(context.Context) (executor.Summary, error) {
	a.ranOnHost = true
	return executor.Summary{}, nil
}
func (a *plainAdapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{}, nil
}

// fakeSandbox records the spec it was asked to run and can be made unavailable.
type fakeSandbox struct {
	unavailable error
	gotSpec     *executor.SandboxSpec
	exitCode    int
}

func (f *fakeSandbox) Preflight(context.Context) error { return f.unavailable }
func (f *fakeSandbox) RunSpec(_ context.Context, _ string, spec executor.SandboxSpec) (executor.CommandResult, error) {
	f.gotSpec = &spec
	return executor.CommandResult{ExitCode: f.exitCode, Stdout: "ok"}, nil
}

func TestDecideSandbox(t *testing.T) {
	optOut := false
	reg := capability.Registry{Executors: []capability.Record{
		{Provider: "opted-out", RequiresSandbox: &optOut, SandboxOptOutReason: "local dev tool"},
		{Provider: "normal"},
	}}
	a := &Activities{CapabilityRegistry: reg}

	if d := a.decideSandbox("normal", false); d.required || d.refusal != "" {
		t.Fatalf("profile not demanding sandbox must not require it: %+v", d)
	}
	if d := a.decideSandbox("normal", true); !d.required || d.refusal != "" {
		t.Fatalf("sandbox-demanding profile must require it: %+v", d)
	}
	if d := a.decideSandbox("opted-out", true); d.refusal != ClassificationSandboxOptOutRefused {
		t.Fatalf("opted-out executor must be refused for a sandbox-demanding profile: %+v", d)
	}
}

func TestRunSandboxed_NoRunnerRefusesFailClosed(t *testing.T) {
	a := &Activities{} // no Sandbox wired
	adapter := &sandboxCapableAdapter{}
	_, out, ok := a.runSandboxed(context.Background(), "e", adapter, worktree.Workspace{}, executor.TaskPacket{})
	if ok {
		t.Fatal("must refuse when no sandbox runner is wired")
	}
	if out.Classification != ClassificationSandboxUnavailable {
		t.Fatalf("want %s, got %s", ClassificationSandboxUnavailable, out.Classification)
	}
	if adapter.ranOnHost {
		t.Fatal("FAIL-OPEN: adapter ran on the host when the sandbox was unavailable")
	}
}

func TestRunSandboxed_UnavailableRefusesFailClosed(t *testing.T) {
	a := &Activities{Sandbox: &fakeSandbox{unavailable: errors.New("engine down")}}
	adapter := &sandboxCapableAdapter{}
	_, out, ok := a.runSandboxed(context.Background(), "e", adapter, worktree.Workspace{}, executor.TaskPacket{})
	if ok || out.Classification != ClassificationSandboxUnavailable {
		t.Fatalf("unavailable sandbox must refuse: ok=%v class=%s", ok, out.Classification)
	}
	if adapter.ranOnHost {
		t.Fatal("FAIL-OPEN: adapter ran on the host when the sandbox was unavailable")
	}
}

func TestRunSandboxed_IncompatibleAdapterRefused(t *testing.T) {
	a := &Activities{Sandbox: &fakeSandbox{}}
	adapter := &plainAdapter{}
	_, out, ok := a.runSandboxed(context.Background(), "e", adapter, worktree.Workspace{}, executor.TaskPacket{})
	if ok || out.Classification != ClassificationSandboxIncompatible {
		t.Fatalf("incompatible adapter must be refused: ok=%v class=%s", ok, out.Classification)
	}
	if adapter.ranOnHost {
		t.Fatal("FAIL-OPEN: incompatible adapter ran on the host")
	}
}

func TestRunSandboxed_HappyRunsInsideSandboxNotHost(t *testing.T) {
	sb := &fakeSandbox{exitCode: 0}
	a := &Activities{Sandbox: sb}
	adapter := &sandboxCapableAdapter{}
	summary, _, ok := a.runSandboxed(context.Background(), "e", adapter, worktree.Workspace{Path: "/ws"}, executor.TaskPacket{})
	if !ok {
		t.Fatal("a healthy sandbox run should succeed")
	}
	if adapter.ranOnHost {
		t.Fatal("FAIL-OPEN: adapter.Run was invoked on the host instead of inside the sandbox")
	}
	if sb.gotSpec == nil || len(sb.gotSpec.Argv) == 0 {
		t.Fatal("the adapter's SandboxSpec must be executed inside the sandbox")
	}
	if summary.Claimed == "" {
		t.Fatal("expected a summary from the sandboxed run")
	}
}

func TestRunSandboxed_NonZeroExitFails(t *testing.T) {
	a := &Activities{Sandbox: &fakeSandbox{exitCode: 1}}
	adapter := &sandboxCapableAdapter{}
	_, out, ok := a.runSandboxed(context.Background(), "e", adapter, worktree.Workspace{}, executor.TaskPacket{})
	if ok || !out.Failed {
		t.Fatalf("a non-zero sandboxed exit must fail the task: ok=%v out=%+v", ok, out)
	}
}
