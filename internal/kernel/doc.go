// Package kernel is the only place sequencing, retries, leases, fencing,
// state, and side effects live (Constitution C4). It hosts the durable
// DeliverPlan Temporal workflow (Constitution C2: Temporal owns durable
// execution history, timers, and sequencing) plus the activities that are
// the sole seam through which the workflow touches the world.
//
// Determinism discipline: workflow.go must never call time.Now, rand, or
// any other non-deterministic source directly — use workflow.Now /
// workflow.SideEffect instead (enforced by TestNoTimeNowInWorkflowFiles in
// lint_test.go). Every side effect — worktree mutation, executor
// invocation, evidence persistence, transition/lease/receipt storage —
// happens inside an activity, never inline in workflow code.
//
// ValidateTask in this package is a STUB pending Task 13
// (internal/verify): it only checks whether ExecuteTask's adapter run
// returned an error, not real command-level verification. It is marked
// TODO at its definition and must not be mistaken for Task 13's honest,
// evidence-based validator.
package kernel
