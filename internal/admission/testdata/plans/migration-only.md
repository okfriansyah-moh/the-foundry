---
id: plan-fixture-migration-only
title: Fixture migration-only
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
  - kind: migration
    target: migrations/0001.sql
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: single migration effect, expect A1 floor.
