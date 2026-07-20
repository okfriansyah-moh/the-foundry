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
