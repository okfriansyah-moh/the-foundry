---
name: coding-standards
description: "Enforce a single, consistent Go coding standard across `internal/*`, `cmd/*`, and tooling, so that any agent's"
---

<!-- ars:source .ai/skills/coding-standards/SKILL.md -->
# Purpose

Enforce a single, consistent Go coding standard across `internal/*`, `cmd/*`, and tooling, so that any agent's
output is indistinguishable in style from any other's.

# Inputs

- `docs/PLAN.md` §C (Conventions) — this repo's own defaults; they win over generic style guides on conflict.
- [Effective Go](https://go.dev/doc/effective_go) and the [Google Go Style Guide](https://google.github.io/styleguide/go/) —
  baseline idiom this repo does not deviate from without a stated reason.

# Repo-specific rules (from `docs/PLAN.md` §C — non-negotiable)

- Every new package has `doc.go` stating its authority limits.
- Errors wrapped with `%w`, never dropped or stringified away from their cause.
- All timestamps UTC.
- Table names `snake_case`; Go package names lower-case single word.
- Secrets never committed — not in code, tests, fixtures, or commit messages.
- Migrations reversible; `down` tested in CI.
- Conventional commits; footer `Task: <N>` on every commit.

# General Go idiom (baseline, only overridden by the rules above)

- Accept interfaces, return concrete types.
- Keep exported surface minimal; unexported by default.
- Name receivers consistently per type; short, not `this`/`self`.
- Prefer `context.Context` as the first parameter for anything that crosses an I/O boundary (DB, Temporal
  activity, HTTP) — never store it in a struct.
- Error strings are lowercase, no trailing punctuation, no capitalized first word unless a proper noun.
- Table-driven tests (`[]struct{ name string; ... }`) for anything with more than two input variations.
- No naked `panic` outside `main` and package-init invariant checks — the kernel process must not crash on
  malformed but reachable input (see `stop-ai-slop` and `security-hardening` for why).
- Run `gofumpt`, `govet`, `staticcheck` before calling anything done — `make lint` wraps all three.

# Anti-Patterns

- Copy-pasting a style from an unrelated ecosystem (e.g. Java getters/setters, Python `_private` prefixes).
- Inconsistent error wrapping — some `%w`, some `%v`, some string concatenation, in the same package.
- A package without `doc.go` "because it's small."
- Local time (`time.Now()` without `.UTC()`) anywhere a timestamp is persisted or compared across processes.
