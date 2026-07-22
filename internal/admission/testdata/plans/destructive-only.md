---
id: plan-fixture-destructive-only
title: Fixture destructive-only
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
  - kind: destructive
    target: drop-table
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: single destructive effect, expect H floor.
