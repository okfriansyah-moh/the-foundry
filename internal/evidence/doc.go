// Package evidence implements tamper-evident, offline-verifiable bundles of
// what actually ran for a task (Constitution C10 — evidence-based
// completion; no self-reported done). A Bundle records commands run, exit
// codes, artifact digests, and state transitions; Store.Verify re-derives
// every hash from bytes on disk rather than trusting stored values, so a
// bundle can never assert its own integrity.
//
// This package performs no side effects beyond writing/reading its own
// content-addressed filesystem tree under $FOUNDRY_DATA_DIR/evidence; it
// does not decide when evidence is recorded (internal/kernel does, per
// Constitution C4) and it is not a network object store — FSStore is the
// only implementation; a remote Store (e.g. S3-backed) is a documented
// future extension, not something this package provides.
//
// Exec role: go-backend (docs/PLAN.md Task 11 / SKP-09).
package evidence
