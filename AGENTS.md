<!-- ars:source .ai/ -->
# delivery-foundry

## Repository Instructions

<!-- ars:source .ai/instructions/authority-boundaries.md -->

# Authority Boundaries

Constitution articles C4 and C5, verbatim from `docs/PLAN.md` §B — this is the enforcement contract every agent's
`Boundaries` section must not contradict.

| #  | Article | Enforced by |
| --- | --- | --- |
| C4 | Kernel owns sequencing, retries, leases, fencing, state, policy, budgets, **all side effects incl. SCM writes** | Task 12, 27, 28 |
| C5 | PEC only proposes waves/dispatch/remediation; prohibition-tested | Task 56 |

## What this means for dispatch

- Only `internal/kernel` performs side effects.
- Only the `go-kernel` agent is ever dispatched against `internal/kernel` or `internal/scm/write`.
- `internal/pec` proposes waves, dispatch, and remediation — it never decides, and it is prohibition-tested against
  ever gaining decision authority.
- No other agent role (`go-backend`, `integration`, `infra`, `web`, `security-review`) may be dispatched against
  `internal/kernel`, `internal/scm/write`, or `internal/pec`'s decision path.

<!-- ars:source .ai/instructions/build-and-test.md -->

# Build And Test

Verbatim from `docs/PLAN.md` §C (Conventions).

## Docker execution model

Every `make <target>` is a thin wrapper around `docker compose run --rm dev <real command>` (long-running services
use `up`/`down` instead). Target names and their meaning never change — only the implementation is containerized,
so every Validation command in every task card (`make test`, `go test ./...`, `bash test/foo.sh`, etc.) is run the
same way whether typed directly or through `make`; commands shown as bare `go test`/`go run`/`bash` are understood
to execute inside `dev` (either via a `make` target or `docker compose run --rm dev <cmd>` directly — an agent may
use either form). Host requirements: Docker Engine/Desktop + Docker Compose v2 + GNU make — no local Go, Node,
Playwright, or database install is ever required. CI builds and runs the identical `dev` image (dev/CI parity — no
"works on my machine"). Go module and build caches live in named Docker volumes so repeated runs stay fast.

## Make targets contract

Created Task 1, extended by later tasks; never renamed:

```
bootstrap up down doctor test lint fitness skp-e2e e2e-github e2e-venture e2e-tenx evidence-verify projection-rebuild
```

Each wraps `docker compose run --rm dev <cmd>` (or `up`/`down`); adding a target never changes an existing one's
name or docker-wrapping pattern.

## Container topology & network policy

Exactly four image lineages exist for the life of the plan, each with one owner task and one stated purpose. No
fifth image or second compose file may be added without a matching row in `docs/PLAN.md` §C — Task 37 lints for
it, so an ad hoc `Dockerfile.whatever` fails CI, not just code review.

| Image | Owner | Purpose | Network |
| --- | --- | --- | --- |
| `dev` | Task 1 | toolchain to build/test/run Foundry itself | full outbound internet |
| `postgres`, `temporal` | Task 4 | `dev`'s runtime dependencies | internal compose network only |
| `foundry-executor-sandbox` | Task 34 | isolates AI-agent-executed task code | default-deny egress + narrow allowlist |
| product template's own image | Task 46 | the venture product's own runtime | governed by the product, not Foundry |
| `foundry` (release) | Task 73 | the shipped `foundry`/`foundryd` binaries | not applicable |

Two hard rules: (1) `deploy/docker-compose.yaml` holds only the long-running dev-time services (`dev`, `postgres`,
`temporal`) — one file, never a second. (2) The network default is open outbound everywhere; nobody hardens `dev`,
`postgres`, or `temporal` by restricting egress. The executor sandbox is the sole deliberate exception: default-deny,
because it's the one place potentially-arbitrary agent-generated code executes.

<!-- ars:source .ai/instructions/ci-remediation.md -->

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

<!-- ars:source .ai/instructions/prompt-caching.md -->

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
  the golden-rule reproducibility test in `scripts/doclint/ai-harness-repro.sh`, wired into `make fitness` /
  `make doclint` per `docs/PLAN.md` Task 37) — that check is also, incidentally, the check that this repo hasn't
  broken its own cacheability.

## What this buys you, concretely

Every one of the five tools named in this repo's provider list (`.ai/manifest.yaml`: `claude`, `codex` — plus
Cursor/Copilot/Antigravity if added later) reads `AGENTS.md`/`CLAUDE.md`-equivalent content as its stable context
on every invocation. Because that content is: (a) composed deterministically from `.ai/`, (b) provably
byte-identical across recompositions, and (c) free of per-request volatility per the rule above — whichever
provider's automatic prefix-caching is in effect gets the best possible hit rate this repo can offer it, without
this repo ever needing to know which provider is asking.

<!-- ars:source .ai/instructions/task-protocol.md -->

# Task Protocol

Summary of `docs/PLAN.md` §A — how to execute the plan with an AI agent.

## Default mode: orchestrator-driven

Once Task 3 (the autonomous PLAN runner) exists, it — not a human — is the default trigger and default
report-recipient for every task from Task 4 onward. The runner reads the Master Index, selects the next eligible
task (`Depends` all ✅), drives its implementation, and reports to itself: it updates the Index, appends to §T, and
moves to the next task.

- **Auto path** — `Risk: Low`/`Med` and `Rev: R1`/`R2` completes end-to-end with zero human steps; reported via a
  non-blocking batched Telegram digest.
- **Gated path** — `Risk: High` or `Rev: R3`/`R4` pauses before commit and sends a blocking Telegram message,
  waiting for `/approve` or `/reject`. No reply ⇒ stays paused; never auto-approves.

## Bootstrap exception

Tasks 1, 2, and 3 are necessarily human-triggered — nothing can orchestrate task selection before the orchestrator
exists. From Task 4 onward, let the runner drive.

## Manual protocol (required for Tasks 1–3; available anytime after as an explicit override)

1. `docs/PLAN.md` is the canonical plan location.
2. The trigger (human for Tasks 1–3, the runner after) gives the agent a task number.
3. The agent implements exactly the card: **Scope** is the allowed surface, **Out of scope** is forbidden
   (violations = review rejection even if tests pass), **Steps** are the ordered path, **Outputs** are exact paths
   that must exist when done.
4. The agent runs the task's **Validation** commands, then repo-wide `make test && make fitness`.
5. The agent reports to whichever party triggered it — changed files, fixes, validation results, skipped commands,
   blockers — flips `Status: ☐ Not started` to `Status: ✅ <date>`, and checks its box in the §D Master Index.
6. Any task whose `Depends` are all ✅ may start; `[P]`-marked tasks with disjoint Outputs may run concurrently,
   bounded to 2 at a time by default.

## No-gaps rule

If an agent hits a genuinely unspecified detail it must (a) check the task's Governing docs, (b) apply §B/§C
defaults, and only then (c) choose the smallest reversible option and record it in the task's Status line as
`decision: <what>`. Inventing scope is never allowed — this applies identically whether the trigger was a human or
the runner.

## Codex Skills

- .agents/skills/ai-vulnerability-defense/SKILL.md
- .agents/skills/code-quality/SKILL.md
- .agents/skills/code-review/SKILL.md
- .agents/skills/coding-standards/SKILL.md
- .agents/skills/frontend-development/SKILL.md
- .agents/skills/lint-final-check/SKILL.md
- .agents/skills/qa-testing/SKILL.md
- .agents/skills/security-hardening/SKILL.md
- .agents/skills/stop-ai-slop/SKILL.md
- .agents/skills/task-implementation/SKILL.md
- .agents/skills/task-review/SKILL.md
- .agents/skills/ui-ux-design/SKILL.md

## go-backend

<!-- ars:source .ai/agents/go-backend/AGENT.md -->
# Agent: go-backend

## Role

Non-authority Go application code: parsers, projections, API handlers, notify engine, spec synthesis, billing.

## Responsibilities

- Implement any task card with `Exec: go-backend`.
- Own packages such as `internal/plan`, `internal/projection`, `internal/notify`, `internal/spec`, and other
  non-authority `internal/*` packages that do not perform side effects.

## Uses

- Skill: task-implementation
- Skill: task-review (self-check before handoff; does not substitute for an independent reviewer — see
  reviewer-independence R0–R4)
- Skill: coding-standards
- Skill: code-quality
- Skill: security-hardening (OWASP Top 10 — especially A05 Injection for parsers/API handlers, A01 for any
  handler touching access control)
- Skill: qa-testing
- Skill: stop-ai-slop

## Boundaries

- Never imports `internal/scm/write` — that authority belongs exclusively to `go-kernel` (Constitution C4).
- Never makes side-effect decisions; side effects and their sequencing are kernel-owned, not backend-owned
  (Constitution C4).

### Subagent
- .codex/agents/go-backend.toml

## go-kernel

<!-- ars:source .ai/agents/go-kernel/AGENT.md -->
# Agent: go-kernel

## Role

Authority-bearing Go code: state, admission, provenance, the kernel workflow, policy compiler, ledgers, PEC,
branch integrator.

## Responsibilities

- Implement any task card with `Exec: go-kernel`.
- Own every package under `internal/kernel`, `internal/state`, `internal/admission`, `internal/provenance`,
  `internal/ledger`, `internal/pec`, `internal/scm/write`.

## Uses

- Skill: task-implementation
- Skill: task-review (self-check before handoff; does not substitute for an independent reviewer — see
  reviewer-independence R0–R4)
- Skill: coding-standards
- Skill: code-quality
- Skill: security-hardening (OWASP Top 10 — this role owns every side effect, so it owns this risk first)
- Skill: ai-vulnerability-defense (OWASP LLM Top 10 — kernel is the enforcement point for excessive-agency
  and prompt-injection containment; LLM01/LLM06 apply directly)
- Skill: stop-ai-slop

## Boundaries

- This is the **only** agent ever dispatched against `internal/scm/write` or side-effect-bearing kernel activities
  (Constitution C4).
- Every task this agent owns is Rev R3 minimum — no exceptions, even for "small" changes.
- Never invents its own risk tier; the task card's `Risk`/`Rev` fields are authoritative (Constitution C6 — a
  plan/task can never self-classify).

### Subagent
- .codex/agents/go-kernel.toml

## infra

<!-- ars:source .ai/agents/infra/AGENT.md -->
# Agent: infra

## Role

Docker, CI, Makefile, migrations tooling, observability plumbing, release tooling.

## Responsibilities

- Implement any task card with `Exec: infra`.
- Own `deploy/`, `.github/workflows/`, the Makefile, `migrations/` tooling (not migration business logic), and
  release/versioning scripts.

## Uses

- Skill: task-implementation
- Skill: task-review (self-check before handoff; does not substitute for an independent reviewer — see
  reviewer-independence R0–R4)
- Skill: coding-standards
- Skill: security-hardening (OWASP Top 10 — A02 Security Misconfiguration and A03 Software Supply Chain
  Failures apply directly to Dockerfiles, CI, and release tooling)
- Skill: stop-ai-slop

## Boundaries

- No business logic under `internal/*` — authority over that tree is scoped by Constitution C4 to the kernel;
  `infra` builds and operates the tooling around it, never the tooling's decisions.
- Never adds a fifth image lineage or a second compose file — the container topology table in
  `.ai/instructions/build-and-test.md` is the single source of truth; Task 37's fitness lint fails CI on drift.

### Subagent
- .codex/agents/infra.toml

## integration

<!-- ars:source .ai/agents/integration/AGENT.md -->
# Agent: integration

## Role

End-to-end harnesses and executor-adapter wiring: Claude Code, Stripe, Fly, and other gated live-service tests.

## Responsibilities

- Implement any task card with `Exec: integration`.
- Build and maintain e2e demo flows (`skp-e2e`, `e2e-github`, `e2e-venture`, `e2e-tenx`) and adapter wiring between
  Foundry and external executors/providers.

## Uses

- Skill: task-implementation
- Skill: task-review (self-check before handoff; does not substitute for an independent reviewer — see
  reviewer-independence R0–R4)
- Skill: qa-testing (this role owns the e2e/gated-live test pyramid tier)
- Skill: security-hardening (gated live tests touch real credentials — A07 authentication failures, A03 supply
  chain apply directly)
- Skill: ai-vulnerability-defense (executor-adapter wiring is the LLM01/LLM06 enforcement boundary in practice)
- Skill: stop-ai-slop

## Boundaries

- Gated tests only (`RUN_GITHUB=1`, `RUN_STRIPE=1`, etc.) — never runs unattended against production credentials.
- Does not perform kernel-authority side effects itself; it wires and tests the adapters the kernel calls
  (Constitution C4 stays with `go-kernel`).

### Subagent
- .codex/agents/integration.toml

## security-review

<!-- ars:source .ai/agents/security-review/AGENT.md -->
# Agent: security-review

## Role

Red-team corpus, sandbox escape tests, authz conformance, R3/R4 sign-off.

## Responsibilities

- Implement any task card with `Exec: security-review`.
- Own the red-team test corpus, sandbox-escape test suites, and authorization-conformance checks; provide the
  independent review required for Rev R3/R4 tasks.

## Uses

- Skill: task-review (this agent's primary tool — it reviews, it does not run task-implementation)
- Skill: code-review (seven-pillar independent review)
- Skill: security-hardening (OWASP Top 10 — the authoritative sign-off, not a self-check)
- Skill: ai-vulnerability-defense (OWASP LLM Top 10 — prompt-injection and excessive-agency sign-off)
- Skill: qa-testing (verifies evidence, doesn't take a self-reported "tests pass" at face value)
- Skill: stop-ai-slop (the standard this role holds every other role's report to)

## Boundaries

- Reviews, never implements — a security-review agent approving its own diff is a fitness violation: it would make
  completion self-reported by the same hand that did the work, which Constitution C10 (evidence-based completion;
  no self-reported done) forbids, and mirrors reviewer-independence R0's rule that self-review never suffices.
- Cannot be the same agent instance/session that authored the diff under review
  (`docs/foundry/docs/security/reviewer-independence.md` R0–R4).

### Subagent
- .codex/agents/security-review.toml

## web

<!-- ars:source .ai/agents/web/AGENT.md -->
# Agent: web

## Role

The venture product template's frontend only (Task 46+).

## Responsibilities

- Implement any task card with `Exec: web`.
- Own the generated product template's UI code — never Foundry's own control plane.

## Uses

- Skill: task-implementation
- Skill: task-review (self-check before handoff; does not substitute for an independent reviewer — see
  reviewer-independence R0–R4)
- Skill: frontend-development
- Skill: ui-ux-design
- Skill: coding-standards
- Skill: security-hardening (client-side checks are UX, not authorization — A01/A07 still apply)
- Skill: stop-ai-slop

## Boundaries

- Never touches Foundry's own control plane — control-plane side effects belong exclusively to the kernel
  (Constitution C4); there is no operator UI for this agent to build, and that surface is deliberately deferred
  (`docs/PLAN.md` §Q).
- Scoped to the product template repository/module, not `internal/*`.

### Subagent
- .codex/agents/web.toml

