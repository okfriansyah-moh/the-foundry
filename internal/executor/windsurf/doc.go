// Package windsurf is the Windsurf CLI executor adapter (docs/PLAN.md Task 89
// / PRV-06). It rounds out GSD Core's named provider list.
//
// It mirrors Task 17's proven claude-code shape via the shared cliexec
// helper: the `windsurf` CLI runs headlessly inside the workspace jail with
// the prompt fed on stdin (never argv), under a fixed package-confined env
// allowlist that never trusts TaskPacket.EnvAllowlist. Adapter selection is
// the kernel's job (Task 85), never this package's.
//
// Kimi and Kilo are deliberately NOT built here — they get capability-registry
// stub rows (availability: unsupported) and docs/notes/kimi-kilo-deferred.md
// only; see that note for the rationale.
package windsurf
