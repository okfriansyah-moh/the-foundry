// Package observe is Foundry's observability seam (docs/PLAN.md Task 31 /
// FND-12, governing doc
// docs/foundry/docs/operations/observability-and-alerts.md §1): a
// Prometheus registry exposing the catalog-subset metrics this task
// instruments, plus opt-in OpenTelemetry trace wiring for foundryd and the
// `foundry` CLI.
//
// Authority: this package performs no side effects of its own beyond
// recording metrics/spans and serving them over HTTP — it never decides
// sequencing, retries, or side effects (Constitution C4 stays with
// internal/kernel). Call sites elsewhere in the tree call this package's
// Record*/Set*/Observe* helpers from inside an activity (a real side-effect
// boundary), never from workflow.go, so a Temporal replay never double-
// counts a metric that was already recorded once via the idempotent
// receipt path (see internal/kernel/idempotency.go's withReceipt).
//
// Every metric here is named exactly after its
// observability-and-alerts.md §1 catalog entry, prefixed foundry_, with
// HELP text that names the catalog metric verbatim so a reader can map
// Prometheus output back to the governing doc without cross-referencing
// source. See docs/notes/observability-metrics.md for the metric -> owner
// -> runbook-stub table the card's Outputs ask for.
//
// This package also implements docs/PLAN.md Task 33 (FND-14)'s
// control-plane self-protection surface (governing doc
// docs/foundry/docs/operations/control-plane-protection.md): a per-
// principal/IP token-bucket rate limiter and bounded intake-queue
// admission middleware (limits.go, both reject-with-429 rather than
// growing or blocking silently), a declarative priority-lane -> Temporal
// task-queue config (queues.go), a brownout mode flag that sheds the
// lowest-priority (learning) lane while keeping recovery/delivery/
// notification admitted (brownout.go), and a dead-letter store + P1-alert
// hook for poisoned work items (deadletter.go). Like this package's
// metrics/tracing half, none of this decides or performs a kernel side
// effect — it only bounds, shapes, and records the load Foundry's own
// control plane admits, per Constitution C4's authority boundary.
package observe
