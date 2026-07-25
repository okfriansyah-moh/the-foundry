package compiler

import "testing"

// TestFieldRulesCoversEveryMergedField pins fieldRules (this task's Step
// (1) schema declaration) against the field names compiler.go's merge
// functions actually use, so the two can never silently diverge — adding a
// new merged field without declaring its rule here fails this test.
func TestFieldRulesCoversEveryMergedField(t *testing.T) {
	want := map[string]Rule{
		"permissions_allowlist":    RuleTightenOnly,
		"deployment_modes":         RuleTightenOnly,
		"budget_ceilings_usd":      RuleTightenOnly,
		"executor_allowlist":       RuleTightenOnly,
		"validation_allowlist_ref": RuleFixed,
		"notification_classes":     RuleTightenOnly,
		"risk_tier_controls":       RuleTightenOnly,
	}
	if len(fieldRules) != len(want) {
		t.Fatalf("fieldRules has %d entries, want %d", len(fieldRules), len(want))
	}
	for field, rule := range want {
		got, ok := fieldRules[field]
		if !ok {
			t.Errorf("fieldRules missing field %q", field)
			continue
		}
		if got != rule {
			t.Errorf("fieldRules[%q] = %q, want %q", field, got, rule)
		}
	}
}
