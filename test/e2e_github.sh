#!/usr/bin/env bash
# `make e2e-github` (docs/PLAN.md Task 27 / FND-08 Step 6): runs
# test/e2e_github's Go program, which drives the real kernel.PushBranch
# entry point (internal/kernel/scmpush.go) against a real Postgres-backed
# lease store and extops ledger, pushing a fresh branch
# "foundry/e2e/<unix-ts>" to a local bare-repo fixture remote.
#
# As named, "e2e-github" suggests a real GitHub remote. This environment
# has no GitHub sandbox-org credentials, so — consistent with this task's
# own local-fixture-first approach (its integration tests already push
# against a real local bare git repo rather than live GitHub) — this
# script substitutes a local bare-repo fixture for GitHub. See
# internal/scm/write/github_gated_test.go for the real-GitHub path,
# written but gated behind RUN_GITHUB=1 and never run in this environment.
#
# Requires a live Postgres reachable at PG_DSN (`make up` starts one;
# deploy/docker-compose.yaml sets PG_DSN for the dev service already).
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"
export PG_DSN

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

go run ./test/e2e_github
