# Windsurf CLI flags snapshot (docs/PLAN.md Task 89 / PRV-06)

Dated snapshot of the `windsurf` CLI invocation this adapter relies on.
Mirrors `docs/notes/claude-code-flags.md`. **Re-verify against the installed
CLI** on any material change — same staleness rule as Task 17.

Snapshot date: 2026-07-28 (unverified against a live install in this
environment — the gated `RUN_REAL_EXECUTOR=1` integration test is the
verification path; adjust `headlessArgs` in `adapter.go` if the real CLI
differs).

## Invocation

```
windsurf --print
```

- Non-interactive/headless mode (no TTY prompt loop).
- Prompt fed on **stdin** (never argv), per Task 17's precedent.

## Environment allowlist

- `PATH`, `HOME` — tool resolution and config discovery.
- `WINDSURF_API_KEY` — provider auth (the deliberately-passed secret).

Every other environment variable is scrubbed before exec.

## Result parsing

Best-effort: stdout captured as untrusted `Summary.Claimed`; no
Windsurf-specific field names leak into `Summary`. Task 13's verifier decides
pass/fail, never this claim.
