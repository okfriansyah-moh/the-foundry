// Package pec is the Plan Execution Coordinator.
//
// Task 56 (TX-03): PEC v1 — wave/remediation proposals + prohibition tests.
//
// PEC is a pure proposal engine (Constitution C5): it proposes waves,
// remediation, and progress reports. It performs NO side effects and
// makes NO decisions; the kernel validates every proposal and acts on its
// own authority.
//
// # Prohibition contract
//
// This package may import only:
//
//   - github.com/okfriansyah-moh/the-foundry/internal/plan
//   - github.com/okfriansyah-moh/the-foundry/internal/state
//   - github.com/okfriansyah-moh/the-foundry/internal/verify (CommandRecord type only)
//   - github.com/okfriansyah-moh/the-foundry/internal/executor (Summary type only)
//
// Importing kernel, scm, ledger, provenance, any database driver, or
// net/http from this package is a fitness violation. This prohibition is
// enforced by the fitlint import-boundary check (see
// scripts/fitness.sh step (h), added by Task 56).
//
// No exported function in this package returns a value that the kernel
// executes without its own independent authorization — proposal types
// carry no capability handles.
package pec
