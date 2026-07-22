---
id: plan-fixture-self-classified
title: Fixture self-classified
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/fixture
    branch: main
tasks:
  - id: t1
    goal: Fixture task
    commands:
      - echo noop
    validation_commands:
      - echo ok
declared_effects:
  - kind: docs
    target: README.md
declared_tier: A0
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: plan declares its own tier
(Constitution C6 hard gate). Must be rejected with ErrSelfClassification and
Tier H regardless of its (harmless-looking) declared effects.
