# Chaos scenarios

| Scenario | Injected fault | Expected outcome | Runbook |
| --- | --- | --- | --- |
| worker-kill | runner disappears mid-step | repair or escalation within SLO | `docs/runbooks/backpressure.md` |
| temporal-outage | workflow service unavailable | honest WAITING / resumed catch-up | `docs/runbooks/projection-lag.md` |
| postgres-outage | projection store unavailable | execution continues, projections stale-labeled | `docs/runbooks/projection-lag.md` |
| provider-storm | repeated 429/5xx | retry/backoff, no duplicate side effects | `docs/runbooks/auto_admission_rate.md` |
| poisoned-task | deterministic validation never recovers | DLQ + alert | `docs/runbooks/auto_promotion_rollback_rate.md` |
