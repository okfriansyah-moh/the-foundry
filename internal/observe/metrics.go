package observe

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry is the process-wide Prometheus registry every metric in this
// package is registered against. It intentionally does not use
// prometheus.DefaultRegisterer: that global would also pick up
// process/Go-runtime collectors from any other package that happens to
// import client_golang, which would make the /metrics output (and the
// "foundry_" prefix count the card's Validation command greps for) depend
// on import order rather than on this package alone. NewMetricsHandler
// serves exactly this registry, plus the standard process/Go collectors
// registered explicitly below.
var Registry = prometheus.NewRegistry()

// Metric instruments for the observability-and-alerts.md §1 catalog
// subset this task's card names. Each Name is "foundry_" + the catalog's
// own metric name (adding a "_total"/"_usd"/"_seconds" suffix only where
// the catalog name itself lacks a unit, per Prometheus naming
// convention); each Help string leads with that catalog name verbatim so
// HELP text traces back to the doc unambiguously (the card's Acceptance
// requirement).
var (
	// WorkflowCompletions implements workflow_completion_rate: DeliverPlan
	// terminal-transition counts by status. The doc's "ratio" is the
	// SUCCEEDED series divided by the sum of all series — Prometheus
	// exposes the counts; Grafana/alerting computes the ratio, same as
	// every other *_rate metric below.
	WorkflowCompletions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "foundry_workflow_completion_rate",
		Help: "workflow_completion_rate: count of DeliverPlan terminal transitions by status label (SUCCEEDED/FAILED/CANCELLED); divide the SUCCEEDED series by the total to obtain the ratio (docs/foundry/docs/operations/observability-and-alerts.md §1).",
	}, []string{"status"})

	// EvidenceRejections implements evidence_rejection_rate: the
	// verifier-honesty signal. Recorded at ValidateTask's existing
	// accept/reject decision point (internal/kernel/activities.go).
	EvidenceRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "foundry_evidence_rejection_rate",
		Help: "evidence_rejection_rate: count of ValidateTask verdicts by result label (accepted/rejected), the verifier honesty signal; divide rejected by the total to obtain the ratio (docs/foundry/docs/operations/observability-and-alerts.md §1).",
	}, []string{"result", "reason"})

	// Retries implements retry_rate: activity invocations Temporal itself
	// retried, by activity name, detected via activity.Info.Attempt > 1
	// (a real Temporal-level retry, not this repo's separate workflow-level
	// "Attempt" field — see internal/kernel/idempotency.go's doc comment
	// for why those two are distinct).
	Retries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "foundry_retry_rate",
		Help: "retry_rate: count of activity invocations where Temporal reports activity.Info.Attempt > 1, by activity name and failure classification; divide by total invocations to obtain the ratio (docs/foundry/docs/operations/observability-and-alerts.md §1).",
	}, []string{"activity", "classification"})

	// ProjectionLagSeconds implements projection_lag_seconds, moved off
	// the plain expvar internal/projection/projector.go used before this
	// task (docs/PLAN.md Task 14 Step 4's documented deferral).
	ProjectionLagSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "foundry_projection_lag_seconds",
		Help: "projection_lag_seconds: age in seconds of the most recently projected workflow_transitions row (Temporal -> PostgreSQL lag, docs/foundry/docs/operations/observability-and-alerts.md §1).",
	})

	// QueueDepth implements queue_depth: bounded-queue depth by queue
	// name. The only queue instrumented today is "notifications"
	// (internal/notify's outbound Postgres-backed queue); the label lets
	// future bounded queues (e.g. Task 33's intake queue) reuse this same
	// metric family without a new Name.
	QueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "foundry_queue_depth",
		Help: "queue_depth: current count of pending rows in a bounded queue, by queue name (docs/foundry/docs/operations/observability-and-alerts.md §1).",
	}, []string{"queue"})

	// DuplicateSideEffectPrevented implements duplicate_side_effect_prevented,
	// moved off the plain expvar internal/kernel/externalop.go used
	// before this task.
	DuplicateSideEffectPrevented = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "foundry_duplicate_side_effect_prevented_total",
		Help: "duplicate_side_effect_prevented: count of WithExternalOp calls short-circuited by an already-executed/reconciled receipt instead of re-running the wrapped side effect — an idempotency proof, not an error signal (docs/foundry/docs/operations/observability-and-alerts.md §1).",
	})

	// ExternalOperationDivergence implements external_operation_divergence,
	// moved off the plain expvar internal/ledger/reconcile.go used before
	// this task.
	ExternalOperationDivergence = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "foundry_external_operation_divergence_total",
		Help: "external_operation_divergence: count of reconciled external operations whose observed state disagreed with the receipt recorded at execution time — saga reconciler findings (docs/foundry/docs/operations/observability-and-alerts.md §1).",
	})

	// CostPerTaskUSD implements cost_per_task, sourced from the cost
	// ledger (internal/ledger/cost) at the point a cost_entries row is
	// incurred or recorded as shadow subscription spend.
	CostPerTaskUSD = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "foundry_cost_per_task_usd",
		Help:    "cost_per_task: distribution of per-task cost-ledger amounts in USD, by provider, recorded when a cost_entries row is incurred or recorded as shadow spend (docs/foundry/docs/operations/observability-and-alerts.md §1).",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25},
	}, []string{"provider"})

	// ProviderWaitingTimeSeconds implements provider_waiting_time. STUB
	// per this task's card ("stub source is acceptable"): today it
	// records the wall-clock duration of an executor adapter's Run()
	// call as a whole (internal/kernel/activities.go's ExecuteTask), which
	// conflates genuine provider wait time with the adapter's own local
	// work — a true wait-vs-work split needs per-call instrumentation
	// inside internal/executor's adapters that does not exist yet. Kept
	// as a distinct metric (not folded into a general "task duration")
	// so a future task can replace only this call site's source without
	// touching the metric's name or consumers.
	ProviderWaitingTimeSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "foundry_provider_waiting_time_seconds",
		Help:    "provider_waiting_time: STUB — approximated as the wall-clock duration of an executor adapter's Run() call, by provider name, pending real per-call wait instrumentation in internal/executor (docs/foundry/docs/operations/observability-and-alerts.md §1).",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider"})
)

func init() {
	Registry.MustRegister(
		WorkflowCompletions,
		EvidenceRejections,
		Retries,
		ProjectionLagSeconds,
		QueueDepth,
		DuplicateSideEffectPrevented,
		ExternalOperationDivergence,
		CostPerTaskUSD,
		ProviderWaitingTimeSeconds,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// Pre-create every label series this process already knows the full,
	// bounded value set for, so /metrics exposes each catalog metric from
	// process start (a *Vec collector emits nothing for a label
	// combination that has never been created) rather than only after the
	// first matching real event — the same property a plain Counter/Gauge
	// gets for free. Instrumentation call sites still create additional
	// series (e.g. an executor name never listed here) on first use.
	for _, status := range []string{"SUCCEEDED", "FAILED", "CANCELLED"} {
		WorkflowCompletions.WithLabelValues(status)
	}
	for _, result := range []string{"accepted", "rejected"} {
		EvidenceRejections.WithLabelValues(result, "")
	}
	for _, activity := range []string{"AcquireWorktree", "ExecuteTask", "RecordEvidence"} {
		Retries.WithLabelValues(activity, "")
	}
	QueueDepth.WithLabelValues("notifications")
	CostPerTaskUSD.WithLabelValues("unspecified")
	ProviderWaitingTimeSeconds.WithLabelValues("unspecified")
}

// RecordWorkflowCompletion increments WorkflowCompletions for a DeliverPlan
// terminal transition. Callers must only call this from a real side-effect
// boundary (an activity), never from workflow.go directly, so Temporal
// replay cannot double-count it — see internal/kernel/activities.go's
// AppendTransition, which is idempotency-guarded per workflow/task/attempt.
func RecordWorkflowCompletion(status string) {
	WorkflowCompletions.WithLabelValues(status).Inc()
}

// RecordEvidenceResult increments EvidenceRejections' accepted or rejected
// series, with reason set only for a rejection (accepted has no reason).
func RecordEvidenceResult(accepted bool, reason string) {
	if accepted {
		EvidenceRejections.WithLabelValues("accepted", "").Inc()
		return
	}
	EvidenceRejections.WithLabelValues("rejected", reason).Inc()
}

// RecordActivityAttempt increments Retries for activity/classification if
// attempt indicates Temporal itself retried this activity invocation
// (attempt > 1 — Temporal's activity.Info.Attempt, an int32, is 1 on the
// first try). A first attempt (attempt <= 1) is intentionally a no-op:
// retry_rate counts retries, not invocations.
func RecordActivityAttempt(activity string, attempt int32, classification string) {
	if attempt <= 1 {
		return
	}
	Retries.WithLabelValues(activity, classification).Inc()
}

// SetProjectionLag sets ProjectionLagSeconds to seconds.
func SetProjectionLag(seconds float64) {
	ProjectionLagSeconds.Set(seconds)
}

// SetQueueDepth sets QueueDepth's depth series to depth.
func SetQueueDepth(queue string, depth int) {
	QueueDepth.WithLabelValues(queue).Set(float64(depth))
}

// IncDuplicateSideEffectPrevented increments DuplicateSideEffectPrevented.
func IncDuplicateSideEffectPrevented() {
	DuplicateSideEffectPrevented.Inc()
}

// IncExternalOperationDivergence increments ExternalOperationDivergence.
func IncExternalOperationDivergence() {
	ExternalOperationDivergence.Inc()
}

// ObserveCostPerTask records one cost-ledger amount (USD) for provider.
func ObserveCostPerTask(provider string, amountUSD float64) {
	CostPerTaskUSD.WithLabelValues(provider).Observe(amountUSD)
}

// ObserveProviderWaitingTime records one executor-adapter Run() duration
// (seconds) for provider. See ProviderWaitingTimeSeconds's doc comment for
// why this is a documented stub, not a true wait-time measurement.
func ObserveProviderWaitingTime(provider string, seconds float64) {
	ProviderWaitingTimeSeconds.WithLabelValues(provider).Observe(seconds)
}
