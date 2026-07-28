# OpenCode CLI flags snapshot (docs/PLAN.md Task 86 / PRV-03)

Dated snapshot of the `opencode` CLI invocation this adapter relies on.
Mirrors `docs/notes/claude-code-flags.md`. **Re-verify against the installed
CLI** on any material change — same staleness rule as Task 17.

Snapshot date: 2026-07-28 (unverified against a live install in this
environment — the gated `RUN_REAL_EXECUTOR=1` integration test is the
verification path; adjust `headlessArgs` in `adapter.go` if the real CLI
differs).

## Invocation

```
opencode run --print
```

- `run` — non-interactive/headless subcommand (no TTY prompt loop).
- `--print` — emit the result to stdout and exit, rather than entering an
  interactive session.
- The task prompt is fed on **stdin** (never argv), per Task 17's precedent —
  keeps the command line a constant argv with no interpolated packet data.

## Environment allowlist

Exhaustive set visible to the subprocess (see `allowedEnv` in `adapter.go`):

- `PATH`, `HOME` — tool resolution and config discovery.
- `OPENCODE_CONFIG` — optional config path override.
- `OPENCODE_API_KEY` — provider auth (the one deliberately-passed secret).

Every other environment variable — including unrelated credentials — is
scrubbed before exec.

## Result parsing

Best-effort: stdout is captured as the (untrusted) `Summary.Claimed`. No
OpenCode-specific field names leak into `Summary`. As with every adapter,
`Summary` is telemetry only — Task 13's verifier decides pass/fail from
real command evidence, never from this claim.
