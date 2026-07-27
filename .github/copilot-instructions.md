# Copilot Instructions

## Scope

- Default workflow target is CI parity, not local-only green runs.
- When `.ai/` files are edited, treat composed artifacts as required outputs.

## CI-Fix Workflow

1. Reproduce failures locally with CI-equivalent commands:
   - `make bootstrap test lint fitness`
   - `make bootstrap doclint` when docs, `.ai/`, compose, Docker, or workflow files changed.
2. Fix only the root-cause files first; avoid unrelated cleanup.
3. If `.ai/` changed, recompose generated artifacts:
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target codex`
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target claude`
4. Rerun affected validations until clean.
5. If a PR exists, confirm checks before reporting done:
   - `gh pr checks <number> --required`

## Reporting Contract

- Always report: changed files, exact validation commands run, and command outcomes.
- Never claim done while required checks are failing or pending.
- If blocked, report blocker plus next concrete remediation step.
