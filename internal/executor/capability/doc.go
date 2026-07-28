// Package capability defines and loads the executor capability registry:
// the refreshable, YAML-backed source of truth for what each executor
// provider can do, so routing (docs/PLAN.md Task 85 / PRV-02) can query
// eligibility instead of hardcoding per-provider feature assumptions.
//
// docs/PLAN.md Task 84 (PRV-01). This package is deliberately inert: it
// makes no network calls (no live provider-API discovery — that is
// §5.7.2 item 1, out of scope this milestone), performs no routing
// decision (that is Task 85's ExecutorSelector), and holds no mutable
// global state. It ships pure data (Record), a strict loader (Load), and a
// pure query function (Registry.Eligible).
//
// The Features vocabulary mirrors provider-execution-classes.md §6.7's
// `capabilities:` set (e.g. "reasoning.adaptive", "tools.strict",
// "context.prompt_cache") but is a deliberately OPEN string set: declaring
// a new capability must never require a Go code change, only a new YAML
// row/feature string.
package capability
