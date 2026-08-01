// Package evidence implements tamper-evident, offline-verifiable bundles of
// what actually ran for a task (Constitution C10 — evidence-based
// completion; no self-reported done). A Bundle records commands run, exit
// codes, artifact digests, and state transitions; Store.Verify re-derives
// every hash from bytes on disk rather than trusting stored values, so a
// bundle can never assert its own integrity.
//
// This package performs no side effects beyond writing/reading its own
// content-addressed evidence stores; it does not decide when evidence is
// recorded (internal/kernel does, per Constitution C4). FSStore stores bundles
// under $FOUNDRY_DATA_DIR/evidence, while S3Store stores the same layout under a
// per-profile S3/MinIO namespace.
//
// Exec role: go-backend (docs/PLAN.md Task 11 / SKP-09).
package evidence
