---
id: generated-spec-2
title: Generated Spec 2
version: "1.0"
repos:
    - alias: product
      branch: main
      url: https://github.com/example/generated-product
tasks:
    - id: task-01-persistence
      goal: Implement persistence requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/persistence
    - id: task-02-billing
      goal: Implement billing requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/billing
declared_effects:
    - kind: billing
      target: billing/*
    - kind: migration
      target: db/*
requested_permissions:
    - kind: repo-write
      target: '*'
budget_usd: 50
---
## Rationale

Generated from specification sections with deterministic effect mapping.
