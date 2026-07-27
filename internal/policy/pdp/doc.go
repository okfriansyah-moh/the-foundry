// Package pdp implements internal/policy.Decider using OPA embedded as a
// library (github.com/open-policy-agent/opa/rego). It answers runtime
// authorization questions — may principal X perform action Y on resource
// Z, given context C and a ResolvedPolicy digest D — against one
// internal/policy/compiler.Resolved policy and one rego bundle, both
// fixed at construction time (docs/PLAN.md Task 23, FND-04).
//
// Authority: this package performs no side effects and never imports
// internal/scm/write (Constitution C4). It never merges configuration
// layers itself and only ever consumes internal/policy/compiler.Resolved
// — never raw LayerPolicy input — so a weakened policy that never passed
// through compiler.Compile has no path into this package (docs/foundry/
// docs/security/authorization-model.md, compiler-vs-PDP split).
package pdp
