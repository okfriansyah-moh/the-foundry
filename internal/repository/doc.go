// Package repository is the authority-neutral owned-repository registry
// (docs/PLAN.md Task 143 / RTC-06).
//
// Authority limits: this package stores and resolves repository declarations
// (provider, canonical URL, ownership, pinned revisions). It never authorizes
// delivery, never writes to canonical clones, and never performs SCM side
// effects — the kernel resolves envelopes and internal/scm/read + worktree
// materialize sources. Transports may name repository IDs only.
package repository
