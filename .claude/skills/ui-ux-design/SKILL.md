---
name: ui-ux-design
description: "Visual and interaction design discipline for the venture product template — companion to `frontend-development`"
---

<!-- ars:source .ai/skills/ui-ux-design/SKILL.md -->
# Purpose

Visual and interaction design discipline for the venture product template — companion to `frontend-development`
(that skill implements; this one decides how it should look and behave). Also the skill a mockup-ingestion task
(Task 43) leans on when translating a mockup into an intentional design, not a literal pixel trace.

# Inputs

- `docs/foundry/docs/workflows/mockup-to-delivery.md` — mockup as first-class entry, Observed/Inferred/Assumed/
  Unresolved labeling (C16).
- WCAG 2.2 (`https://www.w3.org/WAI/WCAG22/quickref/`) — accessibility baseline, not optional polish.

# Principles

- **Intentional over templated.** A generated product should read as a considered design, not a default
  Bootstrap/Tailwind-starter look — pick a typographic scale, a spacing scale, and a small color palette
  (primitive → semantic → component tokens) and use them consistently rather than ad hoc per-component values.
- **Accessibility is a requirement, not a pass.** Color contrast ≥ 4.5:1 for body text, visible focus states,
  no information conveyed by color alone, respect `prefers-reduced-motion` and `prefers-color-scheme`.
- **Responsive by default.** Design for the smallest viewport first; verify layouts don't require horizontal
  scroll on any target breakpoint.
- **Honest about mockup fidelity.** When translating a mockup (C16), explicitly separate what the mockup *showed*
  (Observed), what a reasonable person would *infer* from it (Inferred), what you *guessed* to fill a gap
  (Assumed), and what genuinely needs a human answer (Unresolved) — do not silently upgrade an Assumed detail to
  Observed because it looks fine.
- **Design tokens over inline magic numbers.** Spacing, color, and typography values live in one token source
  (CSS variables or a Svelte-exported tokens module), not scattered `padding: 13px` literals.

# Process

1. Before building, name the design system pieces this page/component needs (spacing scale, palette, type
   pairing) — reuse existing tokens if the template already has them; don't invent a parallel set.
2. Check contrast and keyboard/focus behavior against WCAG 2.2 AA before calling a component done.
3. If working from a mockup, label every design decision per C16 and flag Unresolved items back to the human
   rather than guessing silently.
4. Verify at least one narrow-viewport and one wide-viewport rendering before reporting done.

# Anti-Patterns

- Shipping the generic default look of whatever UI kit was scaffolded, unchanged.
- Color-only status indicators (e.g. a red/green dot with no text/icon alternative).
- Introducing a second design-token source (a new CSS variable set that duplicates existing tokens under
  different names).
- Treating an "Assumed" mockup detail as final without flagging it.
