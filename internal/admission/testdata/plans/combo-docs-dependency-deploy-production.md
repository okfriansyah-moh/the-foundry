---
id: plan-fixture-combo-docs-dependency-deploy-production
title: Fixture combo-docs-dependency-deploy-production
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
  - kind: dependency
    target: go.mod
  - kind: deploy
    target: production
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: docs (A0) + dependency (A1) +
production deploy (A2) fired together; expect highest floor A2 to win over
the lower-floor rules that also fire.
