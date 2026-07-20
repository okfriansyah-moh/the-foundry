# Prompt Caching

## The honest version, first

Prompt caching is a provider-side, wire-protocol mechanism controlled by whichever client makes the API call —
Claude Code's own runtime, the Codex CLI, or the backend behind Cursor, GitHub Copilot, or Google Antigravity. No
file in this repository can flip a "use caching" switch, because none of those tools expose one to repo content —
caching is either automatic (Claude, OpenAI/Codex both cache automatically today) or, where explicit control
exists at all, it's a request-body parameter (`cache_control`, `prompt_cache_options`) set by the calling client,
not something `.ai/`, `AGENTS.md`, or `CLAUDE.md` content can set from inside a markdown file.

What this repo *can* control is the one thing every provider's automatic caching actually keys off: **whether the
context these tools read is a stable, byte-identical prefix across requests.** That's the lever this instruction
file protects.

## How each provider actually caches (as of this writing — re-verify if it's been a year)

- **Claude (Anthropic API / Claude Code):** prefix-match cache. Any byte change anywhere in a cached prefix
  invalidates everything after it. Minimum cacheable prefix is model-dependent (1024–4096 tokens). Default TTL is
  5 minutes (1-hour TTL available). Reads cost ~0.1× base input price; writes cost 1.25×–2×. Verify hits via
  `usage.cache_read_input_tokens` in direct API use. Source:
  `https://platform.claude.com/docs/en/build-with-claude/prompt-caching`.
- **OpenAI (Codex / GPT API):** also a prefix-match cache, automatic on prompts ≥1024 tokens for gpt-4o-and-newer
  models — no code change required. TTL is provider-managed (in-memory: 5–10 min, up to 1h; or a 24h retention
  tier on newer models). Explicit breakpoints (`prompt_cache_options.mode: "explicit"`) exist only for direct API
  callers on GPT-5.6+; the Codex CLI itself doesn't expose this to repo content. Source:
  `https://developers.openai.com/api/docs/guides/prompt-caching`.
- **Cursor, GitHub Copilot, Google Antigravity:** caching (if any) is internal to each product's backend and not
  independently configurable from repository content — there is no public per-repo caching knob for any of them.
  The stable-prefix rule below is still the right thing to do, because it's what makes *any* automatic,
  prefix-keyed cache (which is what every one of these providers runs) actually hit.

## The one rule this repo follows

**Keep `.ai/instructions/*.md`, `.ai/agents/*/AGENT.md`, and `.ai/skills/*/SKILL.md` byte-stable across sessions.**
These compose into the top of `AGENTS.md` / `CLAUDE.md` — the same "system prompt" position every provider's
automatic caching treats as the reusable prefix. This repo's ARES golden rule (`docs/PLAN.md` Task 2: delete
`AGENTS.md`+`CLAUDE.md`, `ars compose`, they come back byte-identical) already guarantees this content is
deterministic and reproducible — that reproducibility is a *prerequisite* for caching to ever hit, not a separate
concern from it.

Concretely, when editing anything under `.ai/`:

- **Never interpolate a timestamp, date, random ID, or session-specific value** into an instruction, agent, or
  skill file. `docs/PLAN.md` §C already requires this discipline for code (`stop-ai-slop`); it applies identically
  to the harness's own source.
- **Keep volatile content out of the cached prefix entirely.** Task-specific values belong in the `.ai/prompts/*.md`
  templates' `{{TASK_NUMBER}}`-style placeholders (filled in per-invocation, not baked into the harness), never in
  `.ai/instructions/`, `.ai/agents/`, or `.ai/skills/`.
- **Serialize any generated list deterministically** (sorted, not insertion-order-from-a-map) — an unstable
  ordering is a silent cache invalidator even when the actual content hasn't changed.
- **Recompose and diff after every `.ai/` change** (`ars compose --target codex` / `--target claude`, then check
  the golden-rule reproducibility test in `scripts/check-ai-harness.sh`) — that check is also, incidentally, the
  check that this repo hasn't broken its own cacheability.

## What this buys you, concretely

Every one of the five tools named in this repo's provider list (`.ai/manifest.yaml`: `claude`, `codex` — plus
Cursor/Copilot/Antigravity if added later) reads `AGENTS.md`/`CLAUDE.md`-equivalent content as its stable context
on every invocation. Because that content is: (a) composed deterministically from `.ai/`, (b) provably
byte-identical across recompositions, and (c) free of per-request volatility per the rule above — whichever
provider's automatic prefix-caching is in effect gets the best possible hit rate this repo can offer it, without
this repo ever needing to know which provider is asking.
