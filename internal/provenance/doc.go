// Package provenance implements the signed ApprovedPlan provenance chain
// (Constitution C7): PlanSubmission -> admission.Decision -> ApprovedPlan.
//
// This package owns the only Ed25519 signing/verification authority for
// ApprovedPlan artifacts. Authorship is provenance, never authorization
// (docs/foundry/docs/security/approval-and-provenance.md §2 rule 1) — every
// load re-verifies the signature, so a tampered row or file is rejected on
// read regardless of who wrote it or when.
//
// Exec role: go-kernel (docs/PLAN.md Task 8 / SKP-06). This package performs
// no side effects outside its own approved_plans table row; kernel workflow
// side effects remain internal/kernel-owned (Constitution C4).
package provenance
