# Reviewer Independence Levels

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** "Independent review" is defined, not assumed. Deterministic checks always override LLM opinion.

## 1. Levels

```text
R0 — implementer self-check; never sufficient for completion
R1 — fresh reviewer session, same provider/model
R2 — different model or provider, independently prompted
R3 — deterministic verification plus an independent (R1/R2) reviewer
R4 — human/domain review where policy requires it
```

High-risk tasks require R3 or R4. Profile policy maps task risk to minimum level.

## 2. Independent reviewer requirements

- separate session and context from the implementer;
- receives the specification, diff, tests, and evidence bundle;
- does not receive the implementer's self-score as authoritative input;
- can never approve its own implementation;
- review prompts, model, and version are recorded;
- disagreements between reviewers are recorded, not silently resolved;
- deterministic checks override any LLM verdict.

## 3. Anti-rubber-stamp metrics

Track per reviewer configuration: rejection rate; agreement-with-implementer rate; defects found post-approval; time-to-review. A reviewer configuration whose rejection rate approaches zero over a sustained window is flagged for recalibration — perpetual approval is a signal of collusion or prompt decay, not quality.
