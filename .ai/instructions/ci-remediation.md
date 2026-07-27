# CI Remediation

Repo-local standard for any CI-fix workflow.

## Required end state

- Local CI-parity commands pass.
- Any `.ai/` edits are recomposed into generated artifacts.
- Required PR checks are green before completion is reported.

## Required commands

1. `make bootstrap test lint fitness`
2. `make bootstrap doclint` when docs, `.ai/`, compose, Docker, or workflow files changed.
3. If `.ai/` changed:
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target codex`
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target claude`
4. If a PR exists:
   - `gh pr checks <number> --required`

## Guardrails

- Prefer smallest-scoped edits tied to observed failing step.
- Do not mark done on local-only success when CI gates are still failing/pending.
- Report exact command outcomes and unresolved blockers.
