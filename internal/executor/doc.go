// Package executor defines the adapter contract every task executor
// implements (Adapter, TaskPacket, Summary, Artifacts) and a subprocess
// harness adapters can use to run commands inside a leased worktree.
//
// This package performs no side effects of its own beyond running the
// subprocess it is explicitly told to run inside an already-leased
// worktree.Workspace; it is not the kernel and never decides what runs or
// when (Constitution C4) — that authority stays in internal/kernel.
//
// Summary is explicitly UNTRUSTED: it is the executor's own self-report of
// what happened and must never be treated as authoritative. A real or fake
// executor can claim success while the underlying commands failed. Task 13
// (deterministic validation runner) is the only source of truth for whether
// a task actually passed; nothing in this package or its callers should
// short-circuit that check based on Summary alone.
package executor
