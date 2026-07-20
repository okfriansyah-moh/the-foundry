# Personal Autonomous Venture Profile

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** This profile grants bounded autonomy explicitly. It is an authorized override of the conservative global defaults (N13), not a contradiction of them.

Global defaults remain: preview `auto`; staging `command`; production `command`. The personal profile grants more inside a declared envelope:

```yaml
profile:
  name: personal-autonomous-venture
  kind: personal

deployment:
  preview:
    mode: auto
  staging:
    mode: auto
  production:
    mode: auto
    requires:
      - personal-profile
      - deployment-target-allowlisted
      - mission-readiness-complete        # see mission-setup-ceremony.md
      - spend-within-envelope             # see operations/cost-accounting.md
      - deterministic-verification-passed
      - synthetic-or-real-canary-passed   # see admission-tiers.md section 4
      - rollback-rehearsed
      - database-changes-reversible-or-backward-compatible
      - no-regulated-data
      - no-new-secret-scope
      - no-authority-expansion
      - health-checks-defined
      - operation-reconciliation-enabled

admission:
  auto_tiers: [A0, A1, A2]
  human_tier: H

promotion:
  auto_levels: [L0, L1-bounded]           # see cumulative-drift-governance.md
  veto_digest: weekly
```

Every requirement above is evaluated by the deterministic policy engine at deployment time; a single unmet requirement downgrades the action to `command` mode and emits `status: WAITING, reason: human-approval`.

Organization profiles MUST NOT inherit from this profile. Non-weakening precedence (N6) still applies: this profile can only exercise autonomy the platform layer has made grantable; it cannot weaken kernel security invariants.
