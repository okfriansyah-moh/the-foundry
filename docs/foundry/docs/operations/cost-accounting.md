# Cost Accounting and Mission Economics

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** Budgets without a ledger are wishes. A USD 100/month mission can be consumed by one frontier-model session in a day; the ledger prevents that.

## 1. Cost ledger

First-class ledger tracking, attributed to workflow, product, and mission:

provider tokens; API requests; subscription-capacity allocation (shadow-costed); model pricing version; infrastructure; deployments; databases; domains; email; observability; third-party APIs; payment fees; refunds; credits; revenue; net contribution.

Cost states are separated and reconciled:

```text
reserved → estimated → incurred → reconciled
shadow (subscription usage priced at equivalent API rates)
```

## 2. Budget envelopes

```yaml
budgets:
  mission_monthly_usd:        # MissionContract
  provider_model_usd:         # per provider/model
  infrastructure_usd:
  experiment_usd:
  support_refund_reserve_usd:
```

## 3. Enforcement

Before execution: reserve expected spend against the relevant envelopes; reject or shrink the work when the reservation cannot be satisfied; per-session caps prevent one model session from consuming the whole monthly mission budget.

After execution: reconcile actual usage against reservations; release unused reservations; attribute reconciled cost to workflow, product, and mission; emit `status: WAITING, reason: budget` when an envelope is exhausted (never silent continuation).

## 4. Mission economics

Mission success uses economics, not revenue alone: revenue; MRR; gross margin; net contribution; cost per improvement cycle; time to payback. These feed the MissionContract success evaluation and the observability catalog.
