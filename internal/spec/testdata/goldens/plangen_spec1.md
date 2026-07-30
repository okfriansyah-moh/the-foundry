---
id: generated-spec-1
title: Generated Spec 1
version: "1.0"
repos:
    - alias: product
      branch: main
      url: https://github.com/example/mission-repo
tasks:
    - id: task-01-apis
      goal: Implement apis requirements [req-apis]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
      files:
        - src/apis
    - id: task-02-responsive
      goal: Implement responsive requirements [req-responsive]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
      files:
        - src/responsive
declared_effects:
    - kind: code
      target: api/*
requested_permissions:
    - kind: code
      target: api/*
    - kind: repo-write
      target: product/**
budget_usd: 75
---
## Rationale

Generated from specification requirement clusters with deterministic, least-privilege effect mapping. Each task is traceable to the requirement IDs in its goal.
