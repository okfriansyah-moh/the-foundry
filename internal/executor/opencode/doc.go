// Package opencode is the OpenCode CLI executor adapter (docs/PLAN.md Task 86
// / PRV-03). OpenCode is GSD Core's most-cited fallback provider and appears
// as the "→ OpenCode" step in provider-execution-classes.md §18's Personal
// routing chain.
//
// It mirrors Task 17's proven claude-code shape via the shared cliexec
// helper: the `opencode` CLI is run headlessly inside the workspace jail with
// the task prompt fed on stdin (never argv), under a fixed package-confined
// environment allowlist that never trusts TaskPacket.EnvAllowlist. All
// OpenCode-specific detail (binary, headless flags, env allowlist) is
// confined to this package; selection of this adapter is the kernel's job
// (Task 85), never this package's.
package opencode
