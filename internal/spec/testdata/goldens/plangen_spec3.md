---
id: generated-spec-3
title: Generated Spec 3
version: "1.0"
repos:
    - alias: product
      branch: main
      url: https://github.com/example/generated-product
tasks:
    - id: task-01-permissions
      goal: Implement permissions requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/permissions
    - id: task-02-authentication
      goal: Implement authentication requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/authentication
    - id: task-03-analytics
      goal: Implement analytics requirements
      commands:
        - make test
      validation_commands:
        - make test
      files:
        - src/analytics
declared_effects:
    - kind: code
      target: analytics/*
    - kind: permission
      target: authn/*
    - kind: permission
      target: authz/*
requested_permissions:
    - kind: repo-write
      target: '*'
budget_usd: 50
---
## Rationale

Generated from specification sections with deterministic effect mapping.
