// Package opportunity is Foundry's first-class, storable, deterministic
// representation of whether an idea is worth building (docs/PLAN.md Task 100,
// OPP-01; Constitution C23).
//
// It models the Phase-A idea schema of
// docs/foundry/docs/workflows/venture-loop.md — ICP, problem/frequency/
// alternative/market/WTP/competitor/distribution/risk evidence, an economic
// buyer, reachable distribution channels and unresolved assumptions — and
// turns that evidence into a deterministic Scorecard (Phase B's seven-weight
// rubric) and a BUILD / VALIDATE-MORE / REJECT Verdict against explicit
// Phase-D thresholds.
//
// Boundary (Task 100): this package is pure data + pure functions + storage.
// It performs no network call, no LLM call, no side effect and no
// authorization decision. Decide returns a *verdict*; only Task 102's
// kernel gate may act on it. This package never imports internal/kernel or
// internal/scm/write.
//
// Determinism is a hard contract: identical input yields a byte-identical
// Scorecard (Scorecard.Canonical), and every weight and threshold is loaded
// from config/opportunity-thresholds.yaml so an evaluation can be changed
// with configuration alone, never a code edit. Labeling is fail-closed and
// mirrors internal/spec.PostPass exactly: this package reuses spec's
// four-value Observed|Inferred|Assumed|Unresolved vocabulary and never
// invents a fifth value.
package opportunity
