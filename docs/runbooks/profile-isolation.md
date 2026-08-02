# Profile isolation runbook (Task 149)

## Trust domain rule

One running security deployment per profile/trust domain. `profile_id` alone is
not a confidentiality boundary — DB role/schema, Temporal namespace, evidence
prefix, secret scope, SCM/deploy/billing identities, Telegram scope, audit
chain, ledger and portfolio must be independently addressable.

## Startup

`foundryd` resolves a single `RuntimeIsolation` manifest and refuses to start
when required identities are missing or when multi-profile single-process
sharing is detected (`RefuseMultiProfileSingleProcess`).

## Cost reconciliation

Missing provider usage is recorded as `unreconciled`, never silent zero.
Threshold breaches freeze new unattended reservations until durable
reconciliation clears the backlog for that profile only.
