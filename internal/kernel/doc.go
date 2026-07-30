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
// ValidateTask in this package runs a task's declared validation commands
// through the real internal/verify.Runner and classifies pass/fail solely
// from Runner records, never from an executor's self-reported Summary
// (Constitution C10; docs/PLAN.md Tasks 13, 99, 104). A task that ran no
// validation commands at all is not a pass: verify.Evaluate returns the
// no-validation-declared classification for an empty record set, so the
// honest-completion enforcement point cannot be bypassed by omission.
package kernel
