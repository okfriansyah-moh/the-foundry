# GitHub Copilot CLI flags snapshot (docs/PLAN.md Task 88 / PRV-05)

Dated snapshot of the `copilot` CLI invocation this adapter relies on. Mirrors
`docs/notes/claude-code-flags.md`. **Re-verify against the installed CLI** on
any material change — same staleness rule as Task 17.

Snapshot date: 2026-07-28 (unverified against a live install in this
environment — the gated `RUN_REAL_EXECUTOR=1` integration test is the
verification path; adjust `headlessArgs` in `adapter.go` if the real CLI
differs).

## Invocation

```
copilot -p --allow-all-tools
```

- `-p` — programmatic/non-interactive prompt mode (no TTY loop).
- `--allow-all-tools` — required for unattended execution so no tool-use
  permission prompt stalls the run (safe only because the workspace is jailed
  per C8 and the env allowlist strips every credential except GitHub auth;
  the executor sandbox (Task 34) is the stronger boundary).
- Prompt fed on **stdin** (never argv), per Task 17's precedent.

## Environment allowlist

- `PATH`, `HOME` — tool resolution and config discovery.
- `GH_TOKEN` / `GITHUB_TOKEN` — GitHub auth (the deliberately-passed secrets;
  Copilot authenticates via GitHub credentials).

Every other environment variable is scrubbed before exec.

## Result parsing

Best-effort: stdout captured as untrusted `Summary.Claimed`; no
Copilot-specific field names leak into `Summary`. Task 13's verifier decides
pass/fail, never this claim.
