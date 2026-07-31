// Package intake implements the staged, resumable idea-intake pipeline behind
// `foundry mission start --idea` (docs/PLAN.md Task 111 / INT-03).
//
// One command takes a raw idea to a running mission:
//
//	idea → opportunity validation → verdict gate → spec synthesis →
//	PLAN generation → admission → approval → mission start
//
// Every stage is persisted with the digests of its inputs and a reference to
// its output artifact, so the pipeline is resumable, inspectable and
// interruptible. Re-running a completed stage returns its recorded output: it
// never re-charges the budget and never re-calls a provider (idempotency).
//
// Two outcomes are terminal by design and are NOT failures: a REJECT verdict
// (OPPORTUNITY_REJECTED) and a VALIDATE-MORE verdict
// (OPPORTUNITY_VALIDATION_REQUIRED) end the run cleanly at stage 2 with the
// operator's next actions printed — "build nothing" is a success.
//
// Authority boundary (Constitution C4/C6): this package is orchestration only.
// It calls the kernel's verdict gate, the admission classifier, the approval
// surface and the mission starter through seams; it makes no authority decision
// of its own, never sets a declared tier, and never approves a plan it
// generated. An H-tier generated plan pauses for strong-auth approval rather
// than auto-approving.
package intake
