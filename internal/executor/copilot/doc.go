// Package copilot is the GitHub Copilot CLI executor adapter (docs/PLAN.md
// Task 88 / PRV-05). Copilot is GSD-named and appears in
// provider-execution-classes.md §18's Personal routing chain (venture-loop.md
// Phase J routes "PR review and documentation" to it — that routing
// assignment is Task 90's concern; this package only builds the executor
// seam).
//
// It mirrors Task 17's proven claude-code shape via the shared cliexec
// helper: the `copilot` CLI runs headlessly inside the workspace jail with
// the prompt fed on stdin (never argv), under a fixed package-confined env
// allowlist that never trusts TaskPacket.EnvAllowlist. Adapter selection is
// the kernel's job (Task 85), never this package's.
package copilot
