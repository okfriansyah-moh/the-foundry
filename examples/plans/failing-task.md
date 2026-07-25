---
id: plan-failing-task
title: Failing Validation Command
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/failing-task
    branch: main
tasks:
  - id: t1
    goal: A task whose validation command deliberately fails
    commands:
      - echo "no-op"
    validation_commands:
      - exit 1
    files: []
declared_effects:
  - kind: code
    target: noop
budget_usd: 0.5
---
## Rationale

Validation command exits 1 on purpose. Used as a fixture for downstream
executor/verification tests that must surface a failing validation command
rather than silently passing.
