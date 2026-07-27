# Fix CI

## Use

Fix failing CI checks with the smallest safe patch and CI-parity validation.

## Inputs

- Failing checks (names/log links)
- Current branch and PR number (if any)
- Relevant workflows and touched files

## Instructions

1. Read failing CI logs and isolate root-cause steps.
2. Reproduce locally using CI-parity commands:
   - `make bootstrap test lint fitness`
   - `make bootstrap doclint` when docs, `.ai/`, compose, Docker, or workflow files changed.
3. Apply minimal, in-scope fixes only.
4. If any `.ai/` file changed, run:
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target codex`
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target claude`
   - rerun doclint/fitness.
5. If a PR exists, verify required checks with `gh pr checks <number> --required`.
6. Report changed files, root causes fixed, validations run, and remaining blockers.

## Check

- Root cause identified for each failing job.
- Validation rerun after every fix.
- No done report with failing/pending required checks.
