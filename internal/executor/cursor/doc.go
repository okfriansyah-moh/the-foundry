// Package cursor is the Cursor CLI executor adapter (docs/PLAN.md Task 88 /
// PRV-05). Cursor is GSD-named and appears in provider-execution-classes.md
// §18's Personal routing chain (venture-loop.md Phase J routes "frontend and
// browser refinement" to it — that routing assignment is Task 90's concern;
// this package only builds the executor seam).
//
// It mirrors Task 17's proven claude-code shape via the shared cliexec
// helper: the Cursor CLI runs headlessly inside the workspace jail with the
// prompt fed on stdin (never argv), under a fixed package-confined env
// allowlist that never trusts TaskPacket.EnvAllowlist. Adapter selection is
// the kernel's job (Task 85), never this package's.
package cursor
