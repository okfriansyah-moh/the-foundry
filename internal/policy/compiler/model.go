package compiler

import "fmt"

// Rule states how a lower layer may modify a field a higher layer already
// set. It is the single vocabulary the merge algorithm reads to decide
// whether a lower layer may narrow, must never change, or may freely set a
// field — see fieldRules below.
type Rule string

const (
	// RuleTightenOnly: a lower layer may only move the field to a more
	// restrictive value; any attempt to widen it is a compile error.
	RuleTightenOnly Rule = "tighten-only"
	// RuleFixed: only the platform layer may set the field; any lower
	// layer that supplies a different value is a compile error, even one
	// that would (by some other measure) be "tighter".
	RuleFixed Rule = "fixed"
	// RuleFree: any layer may set the field to any value; no ordering is
	// enforced.
	RuleFree Rule = "free"
)

// Layer identifies one position in the compile precedence order.
type Layer string

const (
	LayerPlatform Layer = "platform"
	LayerOrg      Layer = "org"
	LayerProfile  Layer = "profile"
	LayerWorkflow Layer = "workflow"
)

// Mode is a deployment execution mode for one environment (docs/foundry/
// docs/architecture/configuration-and-policy.md N13 / 5.11.2).
type Mode string

const (
	ModeAuto     Mode = "auto"
	ModeCommand  Mode = "command"
	ModeDisabled Mode = "disabled"
)

// modeRank orders Mode by restrictiveness. A higher rank is MORE
// restrictive (safer); a tighten-only field may only move to an equal or
// higher rank, never a lower one.
var modeRank = map[Mode]int{
	ModeAuto:     0,
	ModeCommand:  1,
	ModeDisabled: 2,
}

func (m Mode) valid() bool {
	_, ok := modeRank[m]
	return ok
}

// RiskTierControl is the auto-execution gate for one admission tier (keys
// match internal/admission.Tier.String(): "A0".."A2", "H" — this package
// does not import internal/admission to avoid a dependency it does not
// need; it treats the tier label as an opaque map key).
type RiskTierControl struct {
	AutoAllowed   bool `json:"auto_allowed" yaml:"auto_allowed"`
	RequireReview bool `json:"require_review" yaml:"require_review"`
}

// stricterOrEqual reports whether next is at least as restrictive as prev:
// AutoAllowed may only go true->false (never false->true), RequireReview
// may only go false->true (never true->false).
func (prev RiskTierControl) stricterOrEqual(next RiskTierControl) bool {
	if next.AutoAllowed && !prev.AutoAllowed {
		return false
	}
	if !next.RequireReview && prev.RequireReview {
		return false
	}
	return true
}

// Policy is the fully-resolved, concrete configuration: every field has a
// value once the platform layer (the only layer required to set every
// field) has been applied.
type Policy struct {
	PermissionsAllowlist   []string                   `json:"permissions_allowlist"`
	DeploymentModes        map[string]Mode            `json:"deployment_modes"`
	BudgetCeilingsUSD      map[string]float64         `json:"budget_ceilings_usd"`
	ExecutorAllowlist      []string                   `json:"executor_allowlist"`
	ValidationAllowlistRef string                     `json:"validation_allowlist_ref"`
	NotificationClasses    []string                   `json:"notification_classes"`
	RiskTierControls       map[string]RiskTierControl `json:"risk_tier_controls"`
}

// LayerPolicy is what a single layer may declare. Every field is optional:
// nil (or, for ValidationAllowlistRef, a nil pointer) means "this layer
// does not touch this field" and the value inherited from the layer above
// carries through unchanged. Map fields only need to carry the keys a
// layer actually wants to touch — omitted keys inherit unchanged.
type LayerPolicy struct {
	PermissionsAllowlist   []string
	DeploymentModes        map[string]Mode
	BudgetCeilingsUSD      map[string]float64
	ExecutorAllowlist      []string
	ValidationAllowlistRef *string
	NotificationClasses    []string
	RiskTierControls       map[string]RiskTierControl
}

// fieldRules is the schema this task's Step (1) requires: every merged
// field annotated tighten-only, fixed, or free. Field values have
// heterogeneous shapes (string sets, per-key maps, scalars), so
// compiler.go's merge* functions each enforce one field's rule directly
// rather than through a single generic dispatcher; this map is the
// declared source of truth those functions must agree with —
// model_test.go asserts every field compiler.go merges has an entry here
// and that entry matches the Rule the merge function actually enforces.
var fieldRules = map[string]Rule{
	"permissions_allowlist":    RuleTightenOnly,
	"deployment_modes":         RuleTightenOnly,
	"budget_ceilings_usd":      RuleTightenOnly,
	"executor_allowlist":       RuleTightenOnly,
	"validation_allowlist_ref": RuleFixed,
	"notification_classes":     RuleTightenOnly,
	"risk_tier_controls":       RuleTightenOnly,
}

// CompileError names the exact layer and field that violated its rule, per
// this task's acceptance bar ("naming the exact layer and field").
type CompileError struct {
	Layer   Layer
	Field   string
	Message string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("policy compile error: layer %q field %q: %s", e.Layer, e.Field, e.Message)
}
