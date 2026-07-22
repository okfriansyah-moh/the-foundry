---
id: plan-fixture-combo-code-billing
title: Fixture combo-code-billing
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
  - kind: code
    target: billing.go
  - kind: billing
    target: invoices
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: code (A0) + billing (H) fired
together; expect the H floor to win.
