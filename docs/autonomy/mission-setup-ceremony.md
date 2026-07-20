# Mission Setup Ceremony

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative prerequisite** for every autonomous personal mission. The unattended loop may not start until the ceremony completes.

Purpose: front-load every known irreducible human gate to time zero, so autonomous runtime is actually unattended instead of stalling mid-flight on identity, legal, or payment actions.

## 1. Ceremony checklist

Resolve, record, or explicitly defer each item:

Identity and legal: identity verification; legal entity (or explicit sole-proprietor decision); contracts and agreements posture; privacy constraints; allowed markets; prohibited markets; legal constraints.

Money: payment account ownership; KYC; bank/payout destination; spend sources; budget ceilings; customer-support and refund reserve; refund policy.

Infrastructure and access: domain ownership; DNS; email sender/domain; hosting account; provider accounts; approved secret scopes.

Authority: production deployment permission; billing permission; customer-support expectations; incident escalation; shutdown authority.

## 2. MissionReadinessArtifact

```yaml
mission_readiness:
  mission_id: uuid
  completed_gates: [{gate, evidence, completed_at, principal}]
  deferred_gates: [{gate, reason, revisit_when}]
  readiness: pass | fail
  approved_by: principal
  digest: sha256
```

The mission starts only when required readiness checks pass. The artifact is referenced by the deployment policy (`mission-readiness-complete` requirement in the personal profile).

## 3. Unforeseen human gates

Not every gate is predictable. When the loop encounters one:

```text
status: WAITING
reason: unforeseen-human-gate
```

The workflow MUST preserve its checkpoint, state the exact required human action in the notification, and resume automatically after the gate is completed. The gate is appended to the readiness artifact so the next mission's ceremony includes it.
