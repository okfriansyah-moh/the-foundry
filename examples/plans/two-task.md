---
id: plan-two-task
title: Two Task Dependency Chain
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/two-task
    branch: main
tasks:
  - id: t1
    goal: Add the greeting function
    commands:
      - echo "package greet" > greet.go
    validation_commands:
      - go build ./...
    files:
      - greet.go
  - id: t2
    goal: Add a test that depends on t1's greeting function
    depends_on:
      - t1
    commands:
      - echo "package greet" > greet_test.go
    validation_commands:
      - go test ./...
    files:
      - greet_test.go
declared_effects:
  - kind: code
    target: greet.go
  - kind: code
    target: greet_test.go
requested_permissions:
  - kind: repo-write
    target: primary
budget_usd: 2.0
---
## Rationale

Two tasks where the second explicitly depends on the first, exercising
`DependsOn` referential-integrity checks and multi-task digest stability.
