---
id: plan-fixture-deploy-production
title: Fixture deploy-production
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
  - kind: deploy
    target: production
budget_usd: 1.0
---
## Rationale

Fixture for admission classifier golden test: production deploy effect, expect A2 floor.
