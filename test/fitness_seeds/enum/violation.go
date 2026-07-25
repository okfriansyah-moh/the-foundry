// Package fakestate is a seeded fitness violation (docs/PLAN.md Task 18 /
// SKP-16, rule (a)): it declares a const block with 3+ of the six canonical
// workflow status words (Constitution C1) outside internal/state, which
// `fitlint enum` must flag.
package fakestate

const (
	StatePending   = "PENDING"
	StateRunning   = "RUNNING"
	StateSucceeded = "SUCCEEDED"
)
