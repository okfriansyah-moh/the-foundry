// Package claudecode implements executor.Adapter by shelling out to the
// `claude` CLI (Claude Code) in non-interactive print mode. It is
// registered under the name "claude-code" and selected via
// FOUNDRY_EXECUTOR=claude-code (docs/PLAN.md Task 17 / SKP-15).
//
// Verified CLI flags and their source are recorded, dated, in
// docs/notes/claude-code-flags.md — re-verify against the installed
// binary before relying on this adapter unattended (staleness rule,
// docs/foundry/docs/providers/anthropic.md).
package claudecode
