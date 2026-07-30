---
id: generated-spec-3
title: Generated Spec 3
version: "1.0"
repos:
    - alias: product
      branch: main
      url: https://github.com/example/mission-repo
tasks:
    - id: task-01-permissions
      goal: Implement permissions requirements [req-permissions]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
      files:
        - src/permissions
    - id: task-02-authentication
      goal: Implement authentication requirements [req-authentication]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
      files:
        - src/authentication
    - id: task-03-analytics
      goal: Implement analytics requirements [req-analytics]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
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
    - kind: code
      target: analytics/*
    - kind: permission
      target: authn/*
    - kind: permission
      target: authz/*
    - kind: repo-write
      target: product/**
budget_usd: 75
---
## Rationale

Generated from specification requirement clusters with deterministic, least-privilege effect mapping. Each task is traceable to the requirement IDs in its goal.
