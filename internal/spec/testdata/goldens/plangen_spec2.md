---
id: generated-spec-2
title: Generated Spec 2
version: "1.0"
repos:
    - alias: product
      branch: main
      url: https://github.com/example/mission-repo
tasks:
    - id: task-01-persistence
      goal: Implement persistence requirements [req-persistence]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
      files:
        - src/persistence
    - id: task-02-billing
      goal: Implement billing requirements [req-billing]
      validation_optout: true
      validation_optout_reason: generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)
      files:
        - src/billing
declared_effects:
    - kind: billing
      target: billing/*
    - kind: migration
      target: db/*
requested_permissions:
    - kind: billing
      target: billing/*
    - kind: migration
      target: db/*
    - kind: repo-write
      target: product/**
budget_usd: 75
---
## Rationale

Generated from specification requirement clusters with deterministic, least-privilege effect mapping. Each task is traceable to the requirement IDs in its goal.
