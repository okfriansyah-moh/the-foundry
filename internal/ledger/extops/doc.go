// Package extops implements the external-operation ledger (docs/PLAN.md
// Task 26 / FND-07, Constitution C9; governing doc
// docs/foundry/docs/architecture/external-operations.md, N9). It is the
// single durable record of every side effect the kernel makes against a
// system this repository does not control (SCM pushes, deploys, billing
// charges) — reserved before the side effect is attempted, executed once
// the side effect is known to have happened, and reconciled once an
// independent observation confirms (or contradicts) the recorded outcome.
//
// # Relationship to internal/kernel's receipt store (Task 12)
//
// internal/kernel/idempotency.go already provides a ReceiptStore keyed by
// (workflowID, taskID, activity, attempt) — that mechanism makes a
// Temporal *activity invocation* idempotent across replays/retries,
// covering both side-effecting and pure-compute activities alike, and its
// key never leaves the workflow that owns it.
//
// This package is a deliberately separate, more general mechanism for a
// narrower class of thing: side effects against the *external world*.
// Three concrete differences justify not reusing Task 12's store:
//
//   - Key shape: an external operation is identified by (kind, target,
//     idempotency_key) — e.g. "the push to org/repo#branch keyed by this
//     commit" — not by which workflow/task/attempt happened to invoke it.
//     The same external idempotency key may legitimately need to be
//     recognized across a workflow retry that Task 12's per-attempt key
//     would treat as a distinct entry.
//   - State machine: external operations pass through reserved ->
//     executed -> reconciled (or -> failed), so an operation can be
//     independently re-verified against the provider after the fact.
//     Task 12's receipts are write-once and have no such lifecycle.
//   - Reconciliation: N9.2 requires ambiguous/timeout outcomes to be
//     reconciled against the provider's own observed state before retrying
//     — a concept Task 12's store has no notion of.
//
// The kernel-facing wrapper this package's consumers use,
// kernel.WithExternalOp (internal/kernel/externalop.go), is therefore a
// parallel mechanism to kernel.withReceipt, not a layer on top of it.
// Nothing in this package imports internal/kernel/idempotency.go, and
// nothing in that file imports this package.
package extops
