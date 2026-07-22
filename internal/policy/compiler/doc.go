// Package compiler implements docs/PLAN.md Task 22 (FND-03): the policy
// compiler. It folds four configuration layers — platform, organization,
// profile, workflow — in that fixed precedence order into one
// ResolvedPolicy, per docs/foundry/docs/architecture/configuration-and-policy.md
// N6.1. Lower layers may tighten a field a higher layer already set; they
// must never weaken one, and a fixed field must never change at all.
//
// Authority limits: this package makes no runtime authorization decision
// ("may principal X perform action Y") — that split belongs to Task 23's
// OPA PDP, per docs/foundry/docs/security/authorization-model.md. This
// package never imports internal/scm/write.
package compiler
