---
name: task-implementation
description: "Implement one task card at a time from `docs/PLAN.md` while preserving scope, architecture, and validation"
---

<!-- ars:source .ai/skills/task-implementation/SKILL.md -->
# Purpose

Implement one task card at a time from `docs/PLAN.md` while preserving scope, architecture, and validation
discipline.

# Process

1. **Read the card in full.** Goal, Rationale, Depends, Governing docs, Scope, Out of scope, Steps, Outputs,
   Acceptance, Validation, Evidence, Risk, Exec, Rev, Boundary.
2. **Load the dispatched agent's other skills.** The card's `Exec` field names an agent (`.ai/agents/<role>/
   AGENT.md`); its `## Uses` section lists every skill relevant to that role beyond this one — coding standards,
   code quality, security hardening, AI-vulnerability defense, QA, frontend/UI-UX. Apply them, not just this
   skill, while implementing.
3. **Restate scope and out-of-scope** before writing any code — the card's Scope is the allowed surface; Out of
   scope is forbidden even if it would make the task easier (violation = review rejection even if tests pass).
4. **Implement Steps in order.** Do not reorder or skip a step to reach an Output faster; a later step often exists
   to keep an earlier one safe (e.g. writing `doc.go` before code, running `ars validate` before `ars compose`).
5. **Write tests alongside implementation**, not after — production code and its tests land together
   (`.ai/skills/qa-testing/SKILL.md`).
6. **Run the task's Validation commands exactly as written.**
7. **Self-check against the Acceptance list**, item by item, before reporting done — honestly, per
   `.ai/skills/stop-ai-slop/SKILL.md`.

# Anti-Patterns

- Implementing multiple task cards in one pass.
- Pulling forward a future task's Outputs because "it's related."
- Redesigning architecture to make the current task easier.
- Inventing scope for a genuinely unspecified detail instead of following the no-gaps rule
  (`.ai/instructions/task-protocol.md`): check Governing docs → apply §B/§C defaults → smallest reversible choice,
  recorded as `decision: <what>` in the Status line.
- Hand-editing a composed provider artifact (`AGENTS.md`, `CLAUDE.md`) instead of changing `.ai/` and recomposing.
- Skipping repo-wide `make test && make fitness` after task-specific validation passes.
