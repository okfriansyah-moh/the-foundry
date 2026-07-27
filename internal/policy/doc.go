// Package policy is the shared authorization vocabulary between
// internal/policy/compiler (compile-time, non-weakening layer merge —
// Task 22, FND-03) and internal/policy/pdp (runtime yes/no decisions
// evaluated against the compiler's output — Task 23, FND-04).
//
// Authority: this package makes no runtime decisions and performs no side
// effects. Decider is a contract only; internal/policy/pdp.OPADecider is
// its sole implementation. Per docs/foundry/docs/security/
// authorization-model.md's compiler-vs-PDP split, the compiler merges
// configuration layers and the PDP answers "may principal X do action Y on
// resource Z" against the already-merged result — this package never
// re-implements either half, it only names the boundary between them.
package policy

import "context"

// Request is one runtime authorization question: may Principal perform
// Action on Resource, given Context and the digest of the ResolvedPolicy
// (internal/policy/compiler.Resolved.Digest) the caller believes is
// currently authoritative.
type Request struct {
	Principal    string
	Action       string
	Resource     string
	Context      map[string]any
	PolicyDigest string
}

// Decision is a Decider's answer to a Request.
type Decision struct {
	Allow  bool
	Reason string
}

// Decider answers runtime authorization questions. A Decider is bound at
// construction to one compiler.Resolved policy; Decide is a pure function
// of (Request, that bound policy's digest) — same inputs always produce
// the same Decision, with no hidden mutable state (conformance test 2,
// docs/foundry/docs/security/authorization-model.md). A Decider never
// merges configuration layers itself (conformance test 1) and never
// receives raw, unvalidated layer input — only a compiler.Resolved
// (conformance test 3).
type Decider interface {
	Decide(ctx context.Context, req Request) (Decision, error)
}
