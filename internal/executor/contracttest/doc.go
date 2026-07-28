// Package contracttest is the shared executor-adapter contract suite every
// CLI executor package runs against its own adapter (docs/PLAN.md Task 10's
// contract, reused by Tasks 86–89's adapters). Centralizing it means a new
// adapter proves the same properties — registration, workspace-jailed prompt
// writes, environment scrubbing, timeout kill, honest error on nonzero exit
// — without copy-pasting the checks, and a regression in any one adapter is
// caught by the same code that guards all the others.
//
// docs/PLAN.md Task 91 (PRV-08) adds the concurrent fresh-context leak check
// to this package as leak_test.go; that check imports the same Options seam.
package contracttest
