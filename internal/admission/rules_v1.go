package admission

import (
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// Rule is one entry in an ordered, versioned ruleset. Rules are data, not
// control flow: RulesV1 is a plain slice evaluated in order so that adding,
// removing, or reordering a rule is a data change reviewable as a diff,
// never a code change to a switch statement.
type Rule struct {
	// ID is the stable identifier recorded in Decision.RulesEvaluated
	// whenever this rule fires. Never reused for a different rule once
	// published, since it is part of the classifier's replayable output.
	ID string
	// Match reports whether a single effect trips this rule.
	Match func(e plan.Effect) bool
	// TierFloor is the minimum tier this rule imposes when it fires.
	// "Highest floor wins" across all fired rules.
	TierFloor Tier
}

// isProductionTarget reports whether an effect target names a production
// deployment destination. Matching is case-insensitive substring, so
// "production", "prod", and "production-us-east" all match.
func isProductionTarget(target string) bool {
	lower := strings.ToLower(target)
	return strings.Contains(lower, "production") || strings.Contains(lower, "prod")
}

// RulesV1 is the ruleset for ClassifierVersion "admission/v1.0"
// (docs/PLAN.md Task 7 Step 2 / docs/foundry/docs/autonomy/admission-tiers.md
// §2). Order does not affect the result (highest floor wins regardless of
// evaluation order) but is kept stable for readability and diffs.
var RulesV1 = []Rule{
	{
		ID:        "docs-copy-tests",
		TierFloor: TierA0,
		Match: func(e plan.Effect) bool {
			return e.Kind == plan.EffectDocs || e.Kind == plan.EffectCode
		},
	},
	{
		ID:        "dependency-migration-network-secret-deploy",
		TierFloor: TierA1,
		Match: func(e plan.Effect) bool {
			switch e.Kind {
			case plan.EffectDependency, plan.EffectMigration, plan.EffectNetwork, plan.EffectSecret, plan.EffectDeploy:
				return true
			default:
				return false
			}
		},
	},
	{
		ID:        "billing-permission-destructive",
		TierFloor: TierH,
		Match: func(e plan.Effect) bool {
			switch e.Kind {
			case plan.EffectBilling, plan.EffectPermission, plan.EffectDestructive:
				return true
			default:
				return false
			}
		},
	},
	{
		ID:        "production-deploy",
		TierFloor: TierA2,
		Match: func(e plan.Effect) bool {
			return e.Kind == plan.EffectDeploy && isProductionTarget(e.Target)
		},
	},
}
