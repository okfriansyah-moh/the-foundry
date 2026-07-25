// Package admission implements the deterministic AdmissionClassifier
// (Constitution C6): a pure, versioned function from a plan's declared (and,
// from Task 45 onward, detected) effects to an admission tier. The plan
// itself never authorizes its own tier — a self-classifying plan is
// hard-rejected before any ruleset evaluation runs. See
// docs/foundry/docs/autonomy/admission-tiers.md.
//
// Authority: this package is the sole computer of admission tiers. It is
// owned by the go-kernel agent role (docs/PLAN.md Task 7) and performs no
// side effects, I/O, clock reads, or randomness — Classify is a pure
// function of its inputs.
package admission
