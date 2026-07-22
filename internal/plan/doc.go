// Package plan parses executable PLAN.md documents (YAML front matter +
// sectioned Markdown body) into typed structures and exposes a stable
// canonical content digest for provenance binding.
//
// This package performs no admission logic (Task 7) and no plan generation
// (Task 44) — it only parses and hashes.
package plan
