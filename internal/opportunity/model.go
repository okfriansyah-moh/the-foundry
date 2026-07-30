package opportunity

import (
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// Label reuses internal/spec's exact four-value provenance vocabulary
// (Observed|Inferred|Assumed|Unresolved). Task 100 must never invent a fifth
// value, so this is a type alias, not a parallel definition.
type Label = spec.Label

// Label constants, re-exported from internal/spec so callers of this package
// need not import spec directly.
const (
	LabelObserved   = spec.LabelObserved
	LabelInferred   = spec.LabelInferred
	LabelAssumed    = spec.LabelAssumed
	LabelUnresolved = spec.LabelUnresolved
)

// Impact reuses internal/spec's coarse risk-impact tier so Task 45's
// discrepancy machinery can consume opportunity gaps identically to spec gaps.
type Impact = spec.Impact

// Impact constants, re-exported from internal/spec.
const (
	ImpactLow    = spec.ImpactLow
	ImpactMedium = spec.ImpactMedium
	ImpactHigh   = spec.ImpactHigh
)

// ClaimKind enumerates the evidence dimensions Phase A records. This is a
// closed vocabulary: a claim of an unknown kind is invalid.
type ClaimKind string

// The eight claim kinds Phase A's idea schema records.
const (
	KindProblem      ClaimKind = "problem"
	KindFrequency    ClaimKind = "frequency"
	KindAlternative  ClaimKind = "alternative"
	KindMarket       ClaimKind = "market"
	KindWTP          ClaimKind = "wtp"
	KindCompetitor   ClaimKind = "competitor"
	KindDistribution ClaimKind = "distribution"
	KindRisk         ClaimKind = "risk"
)

// Valid reports whether k is one of the eight recognized claim kinds.
func (k ClaimKind) Valid() bool {
	switch k {
	case KindProblem, KindFrequency, KindAlternative, KindMarket,
		KindWTP, KindCompetitor, KindDistribution, KindRisk:
		return true
	default:
		return false
	}
}

// Channel is one reachable distribution channel for an ICP.
type Channel struct {
	Name      string `json:"name" yaml:"name"`
	Kind      string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Reachable bool   `json:"reachable" yaml:"reachable"`
}

// ICP is the ideal-customer-profile: who the buyer is and how they are reached.
type ICP struct {
	Segment           string    `json:"segment" yaml:"segment"`
	Role              string    `json:"role" yaml:"role"`
	EconomicBuyer     string    `json:"economic_buyer" yaml:"economic_buyer"`
	ReachableChannels []Channel `json:"reachable_channels" yaml:"reachable_channels"`
}

// Idea is the raw opportunity input, before evidence is gathered.
type Idea struct {
	ID          string    `json:"id" yaml:"id"`
	Statement   string    `json:"statement" yaml:"statement"`
	SubmittedBy string    `json:"submitted_by" yaml:"submitted_by"`
	SubmittedAt time.Time `json:"submitted_at" yaml:"submitted_at"`
	Source      string    `json:"source" yaml:"source"`
}

// Claim is one labeled piece of opportunity evidence. Label reuses the exact
// four-value spec vocabulary. Untrusted marks a claim produced by the
// research intake path (Task 101), which can never be the sole basis for an
// Observed label.
type Claim struct {
	Kind       ClaimKind `json:"kind" yaml:"kind"`
	Text       string    `json:"text" yaml:"text"`
	Label      Label     `json:"label" yaml:"label"`
	Basis      string    `json:"basis" yaml:"basis"`
	SourceRef  string    `json:"source_ref" yaml:"source_ref"`
	ObservedAt time.Time `json:"observed_at" yaml:"observed_at"`
	Untrusted  bool      `json:"untrusted" yaml:"untrusted"`
}

// Opportunity is a complete, evaluable opportunity: the idea, the ICP, the
// evidence set and the economic envelope Phase D bounds a build within.
type Opportunity struct {
	Idea                       Idea    `json:"idea" yaml:"idea"`
	ICP                        ICP     `json:"icp" yaml:"icp"`
	Claims                     []Claim `json:"claims" yaml:"claims"`
	EstimatedValidationCostUSD float64 `json:"estimated_validation_cost_usd" yaml:"estimated_validation_cost_usd"`
	MVPBudgetUSD               float64 `json:"mvp_budget_usd" yaml:"mvp_budget_usd"`
	MaxActiveBuilds            int     `json:"max_active_builds" yaml:"max_active_builds"`
	// RealValidationSignal records whether a real (non-synthetic) validation
	// signal exists. Task 100 treats this as a plain boolean; Task 102
	// tightens it to require a provenance-backed, allowlisted Task 139 record.
	RealValidationSignal bool `json:"real_validation_signal" yaml:"real_validation_signal"`
}

// Verdict is the deterministic build decision.
type Verdict string

// The three possible verdicts. REJECT is the fail-closed default whenever a
// required threshold is unevaluable.
const (
	VerdictBuild        Verdict = "BUILD"
	VerdictValidateMore Verdict = "VALIDATE-MORE"
	VerdictReject       Verdict = "REJECT"
)

// Valid reports whether v is one of the three recognized verdicts.
func (v Verdict) Valid() bool {
	switch v {
	case VerdictBuild, VerdictValidateMore, VerdictReject:
		return true
	default:
		return false
	}
}
