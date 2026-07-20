# Plan Provenance, Approval, and Admission Artifacts

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** "Approved PLAN.md" is a security claim, not a file-naming convention. Transport (chat upload, repository file, attachment) never confers approval.

## 1. Formal artifacts

Admission produces a chain of immutable, digest-linked artifacts:

```text
PlanSubmission → PlanAnalysis → AdmissionDecision → ApprovedPlan
```

`ApprovedPlan` MUST include:

```yaml
approved_plan:
  plan_id: uuid
  plan_digest: sha256
  source_artifact_digests: [sha256]
  source_repository: {url, revision}     # where applicable
  creator_principal: principal            # provenance
  submitting_principal: principal
  classifier_version: string
  detected_effects: [effect]
  requested_permissions: [permission]
  granted_permissions: [permission]       # subset; never plan-authored
  scope: {repositories, paths, branches}
  risk_tier: A0 | A1 | A2 | H
  budget_envelope: {monthly_usd, workflow_usd}
  data_classification: class
  required_approvals: [role]
  actual_approvers: [{principal, method, at}]
  strong_auth_method: sso | webauthn | signed-artifact
  approved_at: timestamp
  expires_at: timestamp
  revocation: {revoked, revoked_by, reason}
```

## 2. Rules

1. Admission runs for every plan regardless of authorship. Authorship is provenance, never authorization.
2. The plan may request permissions; it must never grant them. Granted permissions are always the policy-validated subset.
3. Approval verification requires at least one authoritative mechanism bound to the plan digest: a signed artifact; an approval state in a trusted system of record; or an authenticated in-band approval command referencing the digest.
4. Admission repair operates only on the approved digest; any repair yields a new digest that either stays within the safe structural repair set or requires re-approval.
5. Plans expire and can be revoked; execution re-checks revocation at every wave boundary.

## 3. Organization / 10x plans

Additionally required:

- validate source repositories and branch revisions;
- validate PRD/RFC/ATDD/TestRail (or equivalent) references and verify source digests;
- require configured engineering/QA/PIC approvals according to risk tier;
- approvals use SSO/WebAuthn or another strong-authenticated surface;
- **Telegram alone is never valid approval for high-risk actions** — it remains a notification and low-risk command channel.

## 4. Author/approver separation

For organization profiles the plan creator and approver SHOULD be distinct principals; profiles MAY require it. For personal profiles, the mission setup ceremony plus the deterministic classifier substitute for second-party review within tiers A0–A2.
