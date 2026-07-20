# Mission Contract and Loop-Exit Semantics

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** Every autonomous mission and every loop MUST have a contract. A loop without an exit condition is an incident, not a feature.

## 1. MissionContract

```yaml
mission:
  id: uuid
  statement: "Reach at least USD 100 in verified net monthly recurring revenue."
  target:
    metric: net_mrr
    source: payment-provider-ledger      # authoritative source, not analytics
    verification: reconciled             # see operations/cost-accounting.md
    amount_usd: 100
    confirmation_window: 30d             # continuous
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: configured
  budget:
    monthly_usd: 100
    total_experiment_usd: 500
  cadence:
    observe: daily
    improve: weekly
  constraints:
    maximum_active_products: 1
    maximum_validation_cycles: 12
    maximum_no_progress_cycles: 4
  pause_when:
    - monthly-budget-exhausted
    - payment-data-unavailable
    - unforeseen-human-gate
  terminate_when:
    - total-budget-exhausted
    - prohibited-market-detected
    - no-viable-candidate-after-max-cycles
  post_success_policy: stop | maintenance | raise-target | continue-growth | start-another-product
```

Net MRR is computed as: active recurring subscriptions − refunds − cancellations − discounts and credits. One payment never triggers success; the confirmation window and minimum-customer rules apply.

## 2. Mission result codes

Mission outcomes are `result_code` values on the canonical lifecycle (never new statuses):

```text
MISSION_TARGET_REACHED        status: SUCCEEDED
MISSION_NO_VIABLE_CANDIDATE   status: FAILED
MISSION_BUDGET_EXHAUSTED      status: FAILED
MISSION_PAUSED_FOR_HUMAN_GATE status: WAITING, reason: unforeseen-human-gate
MISSION_TERMINATED_BY_POLICY  status: CANCELLED
MISSION_KILLED                status: CANCELLED
MISSION_MAINTENANCE_MODE      status: SUCCEEDED, then maintenance loop contract applies
```

Mission success is judged on economics, not revenue alone: revenue, MRR, gross margin, net contribution, cost per improvement cycle, and time to payback (see `operations/cost-accounting.md`).

## 3. Universal loop contract

Every loop in the nested loop model MUST declare:

```yaml
loop_contract:
  trigger:            # what starts an iteration
  cadence:            # how often
  inputs:             # what it reads
  authority:          # what it may do without escalation
  budget:             # spend and iteration bounds
  progress_metrics:   # how progress is measured
  success_condition:  # when the loop's goal is met
  pause_condition:    # when it must wait
  failure_condition:  # when an iteration fails
  exit_condition:     # when the loop terminates
  evidence:           # what it must record
```

Applies to all eight loops:

| Loop | Trigger | Cadence | Exit condition |
|---|---|---|---|
| Portfolio | mission active | daily | MissionContract terminate/success |
| Delivery | admitted plan | per plan | terminal workflow status |
| Recovery | failure/liveness event | event-driven | workflow terminal or escalated |
| Capacity | provider signal | continuous | mission/plan completion |
| Capability | evaluation gap found | weekly | promotion or rejection recorded |
| Learning | evidence accumulated | weekly | promoted, rejected, or budget-frozen |
| Memory | new evidence | daily | curation complete for window |
| Security | scan/event | continuous + scheduled | findings resolved or escalated |

Full per-loop contracts live beside each loop's workflow document.
