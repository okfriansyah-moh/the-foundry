---
id: tenx-fixture-plan
version: v1
title: TenX fixture
tasks:
  - id: atomic-group-1
    goal: Prepare the first handoff group
    commands: ["go test ./internal/kernel -run TenX"]
    validation_commands: ["bash scripts/check_tenx_prohibition.sh ."]
    files: ["internal/kernel/tenx_workflow.go"]
  - id: atomic-group-2
    goal: Prepare the second handoff group
    depends_on: ["atomic-group-1"]
    commands: ["go test ./test/e2e/tenx/... -run Allowed"]
    validation_commands: ["go test ./internal/scm/... -run Contract"]
    files: ["internal/scm/write/bitbucket.go", "internal/scm/read/bitbucket.go"]
---

## Goal
Demonstrate the TenX branch handoff path without PR, merge, staging, or deploy side effects.
