# Human-Touchpoint Inventory

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** "Minimal human touch" is an engineering target with metrics, not a slogan.

## 1. End-to-end inventory (personal venture path)

| Stage | Touchpoint | Classification |
|---|---|---|
| Mission setup | Mission statement, market, budget, revenue target | Initial required setup |
| Mission setup | Payment account, KYC, legal entity, domain, DNS | Irreducible human identity/legal action (front-loaded by ceremony) |
| Opportunity selection | Candidate approval | Automatable after configuration (mission constraints) |
| Specification | Ambiguity outside envelope | Automatable after risk-tiering; H-gate otherwise |
| Plan admission | A0/A1/A2 plans | Fully automatable (deterministic classifier) |
| Plan admission | Tier H plans | Human required |
| Build | Implementation, tests, review | Fully automatable |
| Deployment | Preview/staging/production within profile envelope | Automatable after configuration |
| Billing | Implementation before BillingMaturity | Human required |
| Billing | Bounded implementation after maturity | Automatable after risk-tiering |
| Observation | Metrics, revenue reconciliation | Fully automatable |
| Improvement | L0/bounded-L1 promotions | Non-blocking veto governance |
| Improvement | L2 to L4 promotions | Human required |
| Mission target | Success confirmation | Fully automatable (contract) |
| Maintenance/shutdown | No customers, no data obligations | Automatable after configuration |
| Maintenance/shutdown | Customers exist (refunds, data, legal) | Human required |
| Any stage | Emergency stop, security freeze | Emergency-only human action |

Classifications: fully automatable; automatable after configuration; automatable after risk-tiering; non-blocking veto governance; irreducible human identity/legal action; emergency-only human action.

## 2. Autonomy metrics

The platform MUST report: human interventions per mission cycle; interventions per shipped change; median uninterrupted autonomous runtime; percentage of plans auto-admitted; percentage of deployments automatic; time spent waiting for humans; avoidable vs irreducible gates; veto frequency; rollback frequency after auto-promotion.

These metrics feed the observability catalog (`operations/observability-and-alerts.md`).


---

<!-- Relocated from V11: §16 Human involvement by profile (lines 10556-10575) -->

## 16. Human involvement by profile

| Action | Personal venture | Organization engineering |
|---|---|---|
| Generate ideas | Automatic | Not applicable |
| Read task | Automatic | Automatic |
| Read docs | Automatic | Automatic, permission-scoped |
| Create repository | Automatic | Usually human or approved automation |
| Create branch | Automatic | Automatic |
| Push code | Automatic | Automatic within policy |
| Open PR | Automatic | Automatic |
| Merge PR | Configurable `auto` or `command` | Configurable by profile |
| Deploy preview | Default `auto` | Configurable by profile |
| Deploy production | Default `auto`; may use Telegram `command` | Configurable `auto` or `command` |
| Spend money | Approval required | Not allowed unless separately approved |
| Publish docs | Policy-based | Review required |
| Send work data to Telegram | Allowed personally | Forbidden unless approved |

---

