# Observability metric catalog — owner and runbook map

Date: 2026-07-25

This is Task 31 (FND-12)'s required "docs note mapping metric -> owner -> runbook stub." It covers the
catalog-subset metrics instrumented by `internal/observe` against
`docs/foundry/docs/operations/observability-and-alerts.md` §1's full metric catalog. Every metric name below is
verbatim from that catalog; the governing doc states "An SLO without an alert and runbook does not exist" — the
runbook column here is a **stub** (a first action + escalation path), not a full incident-response doc, which is
future work once real alert thresholds exist (the catalog's own "Alert threshold owner-defined" column is not this
task's Scope).

| Metric (catalog name) | Prometheus name | Owner (agent role) | Source | Runbook stub |
| --- | --- | --- | --- | --- |
| `workflow_completion_rate` | `foundry_workflow_completion_rate` | go-kernel | `internal/kernel/activities.go`'s `AppendTransition`, on a terminal `state.Transition` | A sustained drop in the SUCCEEDED share vs. FAILED/CANCELLED: check the most recent FAILED workflows' `result_code` via `foundry status --consistency=fresh`, then `foundry evidence show <id>` for the failing task's evidence bundle. Escalate to go-kernel if `PROVEN_BLOCKED`/admission-rejected results cluster on one workflow class. |
| `evidence_rejection_rate` | `foundry_evidence_rejection_rate` | go-kernel | `internal/kernel/activities.go`'s `ValidateTask` | A rising rejected share: `ValidateTask` is still Task 13's documented stub (checks only `ExecuteTask`'s own reported failure, not real command-level verification — see `internal/kernel/activities.go`'s `TODO(Task 13)` comment), so first confirm this isn't just executor-reported failures increasing before treating it as a verifier-honesty regression. |
| `retry_rate` | `foundry_retry_rate` | go-kernel | `internal/kernel/activities.go`'s `AcquireWorktree`/`ExecuteTask`/`RecordEvidence`, via Temporal's own `activity.Info.Attempt` | A spike in one activity's retry count: check that activity's genuine infra dependency (worktree host disk, executor process, evidence store) for the underlying fault before assuming Temporal's retry policy itself needs tuning. Per-classification breakdown is not real yet (recorded as `""` — Task 32's retry-policy engine adds it); do not alert on the `classification` label until then. |
| `projection_lag_seconds` | `foundry_projection_lag_seconds` | go-backend | `internal/projection/projector.go`'s `Tick`, moved off the plain `expvar` Task 14 used | A growing lag: check whether the projector loop (`Projector.Run`, hosted in `foundryd`) is still running and whether `workflow_transitions` is growing faster than `Tick`'s `DefaultBatchSize` (500) can drain per interval. |
| `queue_depth` | `foundry_queue_depth{queue="notifications"}` | go-backend | `internal/notify/engine.go`'s `DeliverPending`, via `Store.CountPending` | A growing depth: check `internal/notify.Engine.DeliverPending`'s dead-letter rate and the configured Telegram rate limits (`docs/notes/telegram-limits.md`) — a depth that only grows means deliveries are failing or rate-limited faster than they enqueue. |
| `duplicate_side_effect_prevented` | `foundry_duplicate_side_effect_prevented_total` | go-kernel | `internal/kernel/externalop.go`'s `WithExternalOp` | Informational per the catalog (not alerting) — a nonzero, steady rate is the *expected*, healthy signal that Temporal retries are being correctly deduplicated (Constitution C9). A rate of exactly zero over a long window during active retries would be the actual anomaly worth investigating. |
| `external_operation_divergence` | `foundry_external_operation_divergence_total` | go-kernel | `internal/ledger/reconcile.go`'s `Reconciler.RunOnce` | Any nonzero count: a recorded side-effect receipt disagreed with the provider's actual observed state — treat as a P1 (saga reconciler finding, Constitution C9); inspect the specific `extops.Op` via the ledger's reconcile records before taking any corrective action. |
| `cost_per_task` | `foundry_cost_per_task_usd{provider=...}` | go-kernel | `internal/kernel/activities.go`'s `ReserveBudget`, from `internal/ledger/cost.Store.Reserve`/`RecordShadow` | Records the reservation/shadow estimate, not a reconciled actual (no activity calls `cost.Store.Incur`/`Reconcile` yet — a documented Task 29 follow-up gap, not this task's to close). A sustained rise: check `config/cost-defaults.yaml`'s `default_usd` and whether a specific `provider` is consistently over-costed relative to its budget envelope (`foundry cost show`/`foundry budget raise`). |
| `provider_waiting_time` | `foundry_provider_waiting_time_seconds{provider=...}` | integration | `internal/kernel/activities.go`'s `ExecuteTask`, timing `executor.Adapter.Run` | **STUB per this task's card** — this is the executor adapter's whole `Run()` wall-clock duration, not isolated provider-network wait time; a true wait-vs-work split needs per-call instrumentation inside `internal/executor`'s adapters that does not exist yet. Use this metric only as a coarse per-provider latency trend, not a precise SLO input, until that follow-up lands. |

## Dashboards

`deploy/dashboards/foundry-overview.json` — one panel per row above, auto-provisioned into Grafana's `Foundry`
folder via `deploy/grafana/provisioning/dashboards/dashboards.yml`. Datasource auto-provisioned via
`deploy/grafana/provisioning/datasources/prometheus.yml`.

## Bring-up

`make up PROFILE=obs` starts `prometheus`+`grafana` (compose profile `obs`, `deploy/docker-compose.yaml`) alongside
the existing `postgres`/`temporal` — plain `make up` is unchanged. Prometheus scrapes `dev:9090` — see
`deploy/prometheus/prometheus.yml`'s own comment for exactly which `dev`-service invocations that target resolves
against (`make skp-e2e` specifically passes `--use-aliases` for this reason) and its stated concurrency caveat.

## What this session verified vs. did not

Verified live, this session, against a real running `deploy/docker-compose.yaml` `obs` stack (Docker confirmed
available in this environment): a throwaway binary calling `internal/observe.Serve` from inside a
`docker compose run --rm --use-aliases --service-ports dev` container served `/metrics` with 57 lines matching
`foundry_` (>= this task's Validation threshold of 8); Prometheus's `/api/v1/targets` showed the `foundryd` scrape
job `health: "up"` against `dev:9090`; Prometheus's `/api/v1/query` returned real samples for
`foundry_projection_lag_seconds` and `foundry_workflow_completion_rate`; Grafana's own datasource-proxy query API
(`/api/datasources/proxy/uid/<uid>/api/v1/query`) returned the same real samples, proving the exact query path the
seeded dashboard's panels use.

**Not** verified: an actual `make skp-e2e` run producing real `workflow_completion`/`evidence_rejection` events from
a genuine `DeliverPlan` execution (that script's own Task 19 Status line already documents it as unexercised in a
plain host session; running it fully is outside this task's Steps, which ask only for the OTel/Prometheus/
dashboard/compose wiring, not a full skp-e2e debug run); a literal screenshot image of the Grafana UI — this
environment has no browser/screenshot capability, so this is recorded as a skipped validation step per this
task's card, not fabricated.
