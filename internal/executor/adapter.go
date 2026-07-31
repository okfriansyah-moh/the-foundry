package executor

import (
	"context"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// TaskPacket is the executor-agnostic description of one unit of work: what
// to do, what commands prove it, and what the sandbox is allowed to see.
type TaskPacket struct {
	// PlanID identifies the PLAN.md this task belongs to.
	PlanID string
	// TaskID identifies the task within PlanID.
	TaskID string
	// Goal is the human-readable objective the executor is asked to reach.
	Goal string
	// Commands are the commands the executor runs to do the work, one
	// command line per entry (e.g. "go build ./..."). Subprocess tokenizes
	// each entry into an argv slice itself (whitespace-separated, no shell
	// metacharacter expansion) and execs it directly — no shell is ever
	// invoked, so shell metacharacters in a command string have no special
	// meaning and cannot inject a second command.
	Commands []string
	// ValidationCommands are the commands that prove the work was done,
	// tokenized and run the same way as Commands.
	ValidationCommands []string
	// EnvAllowlist names the only environment variables visible to the
	// subprocess; every other variable in the calling process's environment
	// is scrubbed.
	EnvAllowlist []string
	// TimeoutSec bounds Run; exceeding it kills the entire process group.
	TimeoutSec int
	// Class is the OPTIONAL task-class label (plan.Task.Class, e.g.
	// "frontend", "backend", "review"). It carries no authority — it is a
	// routing hint an adapter may use to pick a per-class model from config
	// (docs/PLAN.md Task 79 / EVO-06, internal/executor/apiexec.ModelPolicy).
	// Empty means "unclassed".
	Class string
	// PhaseHint is an OPTIONAL, NON-AUTHORITATIVE label telling an executor
	// which venture-loop.md phase the kernel considers this task in (H, I,
	// J, K, or M — see internal/kernel's phase derivation). An executor CLI
	// with its own internal phase discipline may use it to shape its own
	// behavior; Foundry never defers any decision to it. Empty means "no
	// hint" and MUST be byte-for-byte behavior-identical to a packet without
	// it (docs/PLAN.md Task 92 / PRV-09). It is one-directional
	// (kernel→executor) and carries no authority: an executor cannot use it
	// to request elevated permissions, skip validation, or alter its
	// EnvAllowlist. It is never a completion signal — Constitution C10 /
	// internal/verify (Task 13) remains the sole judge of done.
	PhaseHint string
}

// Summary is the executor's self-reported account of a Run. It is
// UNTRUSTED: an executor (real or fake) can claim success while the
// underlying commands failed, hung, or were never run at all. Callers must
// verify against actual evidence (Task 13) before treating a task as done.
type Summary struct {
	// Claimed is the executor's self-reported outcome, e.g. "all tests
	// pass". Never authoritative.
	Claimed string
	// ExitNotes is free-form self-reported detail accompanying Claimed.
	ExitNotes string
}

// Artifacts lists the filesystem paths (relative to the workspace) an
// executor produced or touched during Run, for Collect to gather.
type Artifacts struct {
	Paths []string
}

// SandboxSpec is the command an adapter would run, expressed so the kernel can
// run it INSIDE the mandatory sandbox instead of on the host (docs/PLAN.md Task
// 115 / SEC-01). No shell is invoked — Argv is executed directly.
type SandboxSpec struct {
	// Argv is the command and its arguments (argv[0] is the binary).
	Argv []string
	// Stdin is written to the command's standard input, if any.
	Stdin []byte
	// EnvAllowlist names the host environment variables copied into the
	// sandboxed command's environment (the scrub discipline, applied at the
	// container boundary).
	EnvAllowlist []string
	// Timeout bounds the command; zero means the sandbox default.
	Timeout time.Duration
}

// SandboxSpecProvider is an OPTIONAL Adapter capability (additive to the
// three-method contract): an adapter that can express its work as a
// SandboxSpec, so the kernel runs it inside the sandbox. An adapter that does
// not implement it cannot run under a profile whose policy demands sandboxing —
// the kernel refuses rather than falling back to host execution (C24).
type SandboxSpecProvider interface {
	SandboxSpec(ctx context.Context, ws worktree.Workspace, packet TaskPacket) (SandboxSpec, error)
}

// Adapter is the seam every task executor implements: prepare a workspace,
// run the task, collect what it produced. Implementations perform no side
// effects outside the worktree.Workspace they are given.
type Adapter interface {
	// Prepare readies the executor to run packet inside ws. It must not
	// mutate anything outside ws.Path.
	Prepare(ctx context.Context, ws worktree.Workspace, packet TaskPacket) error
	// Run executes the prepared task and returns the executor's own
	// (untrusted) account of what happened.
	Run(ctx context.Context) (Summary, error)
	// Collect gathers the artifacts Run produced.
	Collect(ctx context.Context) (Artifacts, error)
}
