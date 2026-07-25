---
id: plan-fixture-docs-only
title: Fixture docs-only
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
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: single docs effect, expect A0 floor.
