// Package bench implements the V1 acceleration measurement framework
// (docs/PLAN.md Task 134 / ACC-01, Constitution C25). It defines comparable
// metrics, durable RunRecords tagged by arm (control|foundry), report
// rendering with explicit measurement bases, and baseline capture from git
// history (Blocker B12).
//
// This package records and compares measurements; it makes no acceleration
// claim. A metric that cannot be observed in an arm is stored as
// not-measurable, never estimated. Git-derived post-handoff fixes are
// labeled proxy unless linked issue/incident evidence corroborates them.
//
// Authority: measurement and evidence capture only — no side effects, no
// threshold edits at evaluation time (Task 135 owns Foundry-arm comparison).
//
// Exec role: infra (Task 134).
package bench
