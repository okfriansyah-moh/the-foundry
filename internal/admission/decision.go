package admission

import (
	"errors"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// Version is the classifier + ruleset version stamped on every Decision.
// It changes only when the ruleset itself changes (docs/PLAN.md Task 7
// Step 4): bump to "admission/v2.0" etc. when RulesV1 is superseded, never
// in place.
const Version = "admission/v1.0"

// ErrSelfClassification is returned when a plan declares its own admission
// tier. Constitution C6 / the hard gate in Classify: a plan-authored tier is
// never authoritative, and admission fails closed rather than silently
// ignoring the field.
var ErrSelfClassification = errors.New("admission: plan-authored tier ignored")

// PolicyView is the injected read-only view over the policy store that
// supplies the policy digest and per-tier required controls. It is a stub
// interface for this task; real policy-store integration is out of scope
// (docs/PLAN.md Task 7 "Out of scope").
type PolicyView interface {
	// Digest returns the content digest of the currently active policy,
	// persisted alongside the Decision so it can be replayed against.
	Digest() string
	// RequiredControls returns the controls required for admission at the
	// given tier, e.g. "synthetic-canary-gate" for TierA1.
	RequiredControls(t Tier) []string
}

// Decision is the persisted output of the AdmissionClassifier, matching the
// `classifier:` block in docs/foundry/docs/autonomy/admission-tiers.md §1.
type Decision struct {
	ClassifierVersion string        `json:"classifier_version"`
	PolicyDigest      string        `json:"policy_digest"`
	RulesEvaluated    []string      `json:"rules_evaluated"`
	Declared          []plan.Effect `json:"declared_effects"`
	Detected          []plan.Effect `json:"detected_effects"`
	Discrepancies     []plan.Effect `json:"discrepancies"`
	RiskScore         float64       `json:"risk_score"`
	Tier              Tier          `json:"admission_tier"`
	RequiredControls  []string      `json:"required_controls"`
	Explanation       string        `json:"explanation"`
}
