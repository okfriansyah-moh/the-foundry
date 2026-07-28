# Kimi & Kilo executors: deliberately deferred (docs/PLAN.md Task 89 / PRV-06)

Kimi and Kilo are named in GSD Core's provider list but are **explicitly
deprioritized** in Milestone M4. This note records that decision so a future
task can pick them up without rediscovering it — they are *not* silently
dropped.

## What ships this milestone

- Capability-registry rows in `config/executor-capabilities.yaml` for both,
  with `availability: unsupported` (never `Eligible`, never routed to).
- **No adapter code.** There is no `internal/executor/kimi` or
  `internal/executor/kilo` package.

## Behavior when requested

Because they are registered as capability records but have no adapter, the
kernel's `ExecutorSelector` (Task 85) fails **closed** with the distinguishable
`unsupported-executor` reason (classification `policy-violation`) if a plan
task or default ever names `kimi` or `kilo` — a clear, named error, never a
silent no-op or a fallback to another provider. `internal/kernel`'s
`TestExecutorSelect_Unimplemented` asserts this exact behavior.

## Why deprioritized

Kimi and Kilo are less-mainstream CLIs than the six built adapters
(claude-code, opencode, gemini-cli, cursor, copilot, windsurf). Building and
maintaining their adapters — each with its own flags snapshot, env allowlist,
and gated integration test — is not justified until there is demand. Adding
them later is purely additive: create the adapter package (mirroring Task 86),
then flip the registry row from `unsupported` to `supported`.
