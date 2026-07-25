# Purpose

Keep every task's implementation inside its complexity budget — correct, minimal, and maintainable, not merely
passing.

# Principles (in priority order when they conflict)

1. **Correctness** — matches the task card's Acceptance criteria exactly, no more, no less.
2. **KISS** — the simplest design that satisfies the Acceptance criteria wins, even if a "better" design exists
   for a future task that hasn't been asked for yet.
3. **YAGNI** — no interface, config flag, or extension point for a requirement that does not exist in the current
   task card. A second concrete task needing the same shape is what justifies an abstraction — not a guess that
   one might arrive.
4. **DRY**, but only after duplication is real and repeated (rule of three) — two similar 5-line blocks are not
   yet a shared helper; a third occurrence is.
5. **SOLID**, applied pragmatically — Single Responsibility and Dependency Inversion matter most in
   `internal/kernel` and `internal/pec` where authority boundaries are the whole point (see
   `.ai/instructions/authority-boundaries.md`); Open/Closed and Interface Segregation matter most in adapters
   (`internal/scm`, `internal/executor`) that must support more than one provider.

# Complexity checks

- Prefer functions under ~40 lines and files under ~400 lines; a package that's outgrown that is a signal to
  split by responsibility, not to keep appending.
- Cyclomatic complexity: if a function needs more than ~4 levels of nested `if`/`for`, extract named helpers —
  the extraction should make the control flow readable without needing the original context.
- No global mutable state outside `internal/kernel`'s explicitly modeled state machine (Constitution C1/C4) — a
  package-level `var` that isn't a `sync.Once`-guarded singleton or a compiled-in constant is a smell.
- No speculative plugin/registry system until at least two concrete implementations exist that need one.

# Process

- Before writing code, name the concrete task-card requirement each new type/function satisfies. If you can't, it
  doesn't belong in this change.
- After writing code, re-read it as if reviewing someone else's PR: would you ask "why does this exist?" about any
  line? If yes, either justify it in a comment (only if the WHY is genuinely non-obvious) or delete it.
- Prefer three similar lines over a premature one-line-saving abstraction — see `stop-ai-slop` for the fuller
  rationale.

# Anti-Patterns

- Building a generic plugin/strategy framework for a single caller.
- Config flags or feature toggles for behavior no task card asked to be toggleable.
- Defensive `nil` checks and error handling for states the type system or the kernel's own invariants already
  rule out.
- "While I was in here" refactors of adjacent code outside the task's Scope.
