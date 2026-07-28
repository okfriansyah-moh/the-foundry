---
id: generated-spec-1
title: Generated Spec 1
version: "1.0"
repos:
    - alias: product
      branch: main
      url: https://github.com/example/generated-product
tasks:
    - id: task-01-apis
      goal: Implement apis requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/apis
    - id: task-02-responsive
      goal: Implement responsive requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/responsive
declared_effects:
    - kind: code
      target: api/*
requested_permissions:
    - kind: repo-write
      target: '*'
budget_usd: 50
---
## Rationale

Generated from specification sections with deterministic effect mapping.
