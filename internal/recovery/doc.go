// Package recovery gives operators a stable, human-inspectable fingerprint
// of how far a DeliverPlan workflow has durably progressed (docs/PLAN.md
// Task 16 / SKP-14, Constitution C22).
//
// Temporal's own workflow history is the actual durable checkpoint — a
// killed and restarted worker resumes exactly where Temporal's history
// says it left off, and every side effect the kernel performs is guarded
// by internal/kernel's idempotency receipts (internal/kernel/idempotency.go),
// not by anything in this package. This package does not read, write, or
// replay anything; it only computes a deterministic CheckpointID from the
// (last completed task ID, evidence IDs) pair internal/kernel's workflow
// code already tracks, so that ID can be stamped onto each
// internal/state.Transition for operator visibility (e.g. "did this
// restart pick up after task-3, or task-4?") without inventing a second,
// competing persistence mechanism.
package recovery
