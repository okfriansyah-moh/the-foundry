// Package verify is the deterministic validation runner: truth about a
// task comes from the exit codes of commands it actually ran, never from
// an executor's self-reported Summary (Constitution C10; docs/PLAN.md
// Task 13 / SKP-11).
//
// Runner.Run tokenizes and execs each command argv-style with no shell, so
// shell metacharacters in a command string are inert. Allowlist refuses
// any command whose first token isn't approved before it ever runs.
// Evaluate turns the resulting []CommandRecord into the one honest
// pass/fail verdict and failure Classification a caller (internal/kernel's
// ValidateTask activity) may act on.
//
// This package performs no side effects beyond running the commands it is
// explicitly given, inside the workspace it is given; it does not decide
// when validation runs (internal/kernel does, per Constitution C4).
//
// Exec role: go-backend (docs/PLAN.md Task 13 / SKP-11).
package verify
