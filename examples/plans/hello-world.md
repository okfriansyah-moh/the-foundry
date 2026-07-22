---
id: plan-hello-world
title: Hello World
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/hello-world
    branch: main
tasks:
  - id: t1
    goal: Print hello world and add a test
    commands:
      - echo "hello world"
    validation_commands:
      - go test ./...
    files:
      - main.go
declared_effects:
  - kind: code
    target: main.go
requested_permissions:
  - kind: repo-write
    target: primary
budget_usd: 1.0
---
## Rationale

Smallest possible executable plan: one task, one command, one validation
command. Used as the golden happy-path fixture for `internal/plan` parsing
and digest tests.
