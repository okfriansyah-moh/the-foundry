package executor

import (
	"context"

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
