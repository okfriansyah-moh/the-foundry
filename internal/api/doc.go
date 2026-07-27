// Package api implements foundryd's HTTP API (docs/PLAN.md Task 36 /
// FND-17): a REST surface under /v1 mirroring the `foundry` CLI's
// submit/approve/status/evidence/profiles commands, OIDC-protected via a
// session JWT (internal/authn, Task 25) and authorized per route through
// internal/policy.Decider (the OPA-backed PDP, Task 23).
//
// Authority: this package performs no kernel-owned side effects
// (Constitution C4) and makes no admission/authorization decisions of its
// own — it is a thin HTTP transport over the same non-authority seams the
// CLI already calls directly (internal/plan, internal/provenance,
// internal/evidence, internal/profile, internal/authn). It never imports
// internal/kernel or internal/scm/write. Approval step-up semantics
// (WebAuthn, PlanContext.RequiresStrongAuth) are entirely internal/authn's
// (Task 25); this package only wires that existing handler onto a route
// and supplies its PlanContextResolver.
//
// Every route requires a valid session JWT (see principalFromRequest) and
// a PDP Allow decision (see authorize) before any handler body runs.
package api
