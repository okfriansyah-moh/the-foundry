// Package projection builds the rebuildable PostgreSQL read model of
// workflow status from the workflow_transitions stream Task 12's kernel
// appends to (docs/PLAN.md Task 14, SKP-12; Constitution C3).
//
// Governing doc: docs/foundry/docs/architecture/data-consistency.md §2
// (projection contract).
//
// Authority boundary: this package is read-only observability, never
// execution authority. It makes no control decisions, and nothing in
// internal/kernel may read workflow_status_projection to sequence or decide
// anything — sequencing, retries, leases, and all side effects remain
// exclusively kernel-owned (Constitution C4). Projections are rebuildable
// at any time from workflow_transitions; Rebuild is that routine, tested
// operation (data-consistency.md §2: "rebuild is a routine, tested
// operation").
package projection
