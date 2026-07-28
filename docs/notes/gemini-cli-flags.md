# Gemini CLI flags snapshot (docs/PLAN.md Task 87 / PRV-04)

Dated snapshot of the `gemini` CLI invocation this adapter relies on. Mirrors
`docs/notes/claude-code-flags.md`. **Re-verify against the installed CLI** on
any material change — same staleness rule as Task 17.

Snapshot date: 2026-07-28 (unverified against a live install in this
environment — the gated `RUN_REAL_EXECUTOR=1` integration test is the
verification path; adjust `headlessArgs` in `adapter.go` if the real CLI
differs).

## Invocation

```
gemini --prompt-interactive=false
```

- Non-interactive/headless mode (no TTY prompt loop).
- The task prompt is fed on **stdin** (never argv) per Task 17's precedent.

## Environment allowlist

Exhaustive set visible to the subprocess (see `allowedEnv` in `adapter.go`):

- `PATH`, `HOME` — tool resolution and config discovery.
- `GEMINI_API_KEY` / `GOOGLE_API_KEY` — provider auth (the deliberately-passed
  secrets).
- `GOOGLE_GENAI_USE_VERTEXAI` — optional backend selector.

Every other environment variable is scrubbed before exec.

## Capability mapping

Gemini's server-side caching maps to `context.prompt_cache`; tool-search maps
to `tools.strict` — existing §6.7 capability strings, not a parallel
vocabulary (Task 87 governing-docs requirement).

## Result parsing

Best-effort: stdout captured as untrusted `Summary.Claimed`; no
Gemini-specific field names leak into `Summary`. Task 13's verifier decides
pass/fail, never this claim.
