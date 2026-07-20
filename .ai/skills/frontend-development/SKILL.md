# Purpose

Frontend implementation discipline for the venture product template (Task 46+) — SvelteKit front, Go API back,
per `docs/PLAN.md` Task 46. Scoped exclusively to the product template; the `web` agent never builds Foundry's
own control-plane UI (there isn't one — §Q, deliberately deferred).

# Inputs

- `docs/PLAN.md` Task 46 (template shape: SvelteKit + Go API, Postgres via env DSN, `/healthz`/`/readyz`,
  Stripe test-mode stubs, Playwright smoke journey, `make dev test e2e`).
- `docs/foundry/docs/workflows/mockup-to-delivery.md` — how a mockup becomes an Observed/Inferred/Assumed/
  Unresolved spec (C16) that this skill's output must satisfy.
- `ui-ux-design` (companion skill — visual/interaction design; this skill is implementation).

# Baseline practice

- **Component structure:** small, single-purpose Svelte components; co-locate a component's styles and logic;
  avoid a global CSS dumping ground.
- **State:** prefer Svelte stores/runes scoped to the feature that owns them; no ad hoc global mutable state for
  convenience.
- **API contract:** the Go API is the source of truth for validation and business rules — never duplicate
  authorization or business-rule logic in the frontend as the *only* enforcement; client-side checks are UX,
  not security (see `security-hardening` A01/A07 — broken access control isn't fixed by hiding a button).
- **Accessibility:** semantic HTML first; every interactive element keyboard-reachable and screen-reader labeled;
  target WCAG 2.2 AA at minimum (see `ui-ux-design`).
- **Performance:** avoid unnecessary client-side JS for content that can be server-rendered; lazy-load anything
  not needed for first paint.
- **Testing:** Playwright smoke journey (per Task 46) covers the golden path end-to-end against a real running
  instance — not just component-level snapshot tests.

# Process

1. Confirm which spec (mockup-derived or hand-written) this component/page satisfies, and which of its
   Observed/Inferred/Assumed/Unresolved labels (C16) apply — an "Assumed" detail should be visibly flagged in the
   implementation notes, not silently decided.
2. Build the component against the real Go API contract (even a stub), not a hardcoded fixture that will diverge.
3. Verify keyboard navigation and screen-reader labels manually, not just via a11y-lint passing.
4. Run the Playwright smoke journey before reporting done.

# Anti-Patterns

- Client-side-only authorization/validation for anything security-relevant.
- A component that only works with mouse input.
- Introducing a second frontend framework/library "for this one page" — SvelteKit is the template's one stack.
- Shipping a mockup-derived "Assumed" detail as if it were "Observed" — that's a C16 violation, not a shortcut.
