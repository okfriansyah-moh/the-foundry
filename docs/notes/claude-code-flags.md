# Claude Code CLI flags — verified snapshot

Date: 2026-07-21
Verification method: ran the real, installed `claude` binary directly in the implementation environment —
`claude --version` and `claude --help` — and read its own `--help` output. Not sourced from
`docs/foundry/docs/providers/anthropic.md` or any other written doc, per that doc's staleness rule (§5.8, header
note: "every feature name, limit, and behavior... MUST be re-verified against official Anthropic documentation at
implementation time").

## Binary confirmed present

```
$ claude --version
2.1.211 (Claude Code)
```

## Flags this adapter (`internal/executor/claudecode/adapter.go`) relies on — confirmed via `claude --help`

| Flag | Confirmed text from `--help` |
| --- | --- |
| `-p, --print` | "Print response and exit (useful for pipes)... Only use this in directories you trust." |
| `--output-format <format>` | "(only works with --print): 'text' (default), 'json' (single result), or 'stream-json'" |
| `--permission-mode <mode>` | "Permission mode to use for the session (choices: 'acceptEdits', 'auto', 'bypassPermissions', 'manual', 'dontAsk', 'plan')" |
| `--dangerously-skip-permissions` / `--allow-dangerously-skip-permissions` | exists, but this adapter uses `--permission-mode bypassPermissions` instead — the mode flag is the documented non-interactive equivalent without the "dangerously" naming, and both are confirmed present on this binary. |

There is **no dedicated "prompt file" or `--stdin` flag** — the CLI takes the prompt either as a trailing
positional argument or (by long-standing CLI convention, `cat file | claude -p`) via stdin when stdout is piped.
This adapter feeds the prompt file's contents on **stdin** via the new `executor.RunSubprocessWithStdin` helper
(`internal/executor/subprocess.go`), never by folding file content into the argv command string — this is the
"file-based, not inline shell string" requirement from the Task 17 card.

## What was NOT independently verified in this session

- The exact JSON schema of `--output-format json`'s single-result object (field names `result`, `is_error`,
  `session_id`, `num_turns`, `duration_ms`, `total_cost_usd`, `usage`) is **not** confirmed against a live response
  in this session — no real prompt was sent to the real `claude` CLI (would cost real API usage against a real
  Anthropic account; not attempted without clearer authorization than "the binary happens to be on PATH"). The
  adapter's `parseSummary` (`internal/executor/claudecode/adapter.go`) is defensive: an unrecognized/evolved JSON
  shape falls back to raw stdout as `Summary.Claimed` rather than failing the run, precisely because this schema is
  unverified telemetry, not a contract.
- Whether stdin-as-prompt (no positional arg, no TTY) is actually honored by this exact binary version was not
  confirmed with a live invocation, for the same reason.
- `RUN_REAL_EXECUTOR=1 go test ./internal/executor/claudecode/ -run Integration` was **not run** in this session.
  `internal/executor/claudecode/integration_test.go` is written and gated correctly (skips cleanly without the
  flag; skips if `claude` isn't on PATH), ready to run once someone explicitly authorizes spending real API usage
  against a real account to exercise it.

## Re-verify before unattended use

Per Blocker B8 (`docs/PLAN.md` §"Blockers": "Claude Code automated-use constraints (ToS/limits) — verify before
Task 17 runs unattended; fake executor unblocks everything meanwhile") — this adapter must not be flipped on for
unattended/production dispatch until B8 itself is separately verified. This note only covers CLI flag behavior, not
ToS/rate-limit constraints.
