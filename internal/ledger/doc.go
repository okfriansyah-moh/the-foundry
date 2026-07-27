// Package ledger holds the durable ledgers behind every kernel-owned side
// effect (Constitution C9/C19). The external-operation ledger's data
// layer lives in the extops subpackage; this package's own reconcile.go
// (docs/PLAN.md Task 26/FND-07) is the reconciler that compares expected
// vs. observed state for operations whose kind has a registered prober.
package ledger
