package opportunity

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is the canonical location of the scoring/threshold config.
const DefaultConfigPath = "config/opportunity-thresholds.yaml"

// Weights are Phase B's seven positive scoring weights plus the two penalty
// weights. They are loaded from config and never hardcoded (docs/PLAN.md
// Task 100 Step 3).
type Weights struct {
	PainSeverity          float64 `yaml:"pain_severity"`
	WTPEvidence           float64 `yaml:"wtp_evidence"`
	ReachableDistribution float64 `yaml:"reachable_distribution"`
	FounderFit            float64 `yaml:"founder_fit"`
	SpeedToMVP            float64 `yaml:"speed_to_mvp"`
	RecurringRevenue      float64 `yaml:"recurring_revenue"`
	Defensibility         float64 `yaml:"defensibility"`
	RiskPenalty           float64 `yaml:"risk_penalty"`
	CostPenalty           float64 `yaml:"cost_penalty"`
}

// Thresholds are Phase D's explicit selection gates.
type Thresholds struct {
	MinimumTotalScore            float64 `yaml:"minimum_total_score"`
	MinimumDistributionScore     float64 `yaml:"minimum_distribution_score"`
	MinimumPaymentEvidenceScore  float64 `yaml:"minimum_payment_evidence_score"`
	MustHaveRealValidationSignal bool    `yaml:"must_have_real_validation_signal"`
	MaximumMVPBudgetUSD          float64 `yaml:"maximum_mvp_budget_usd"`
	MaximumActiveBuilds          int     `yaml:"maximum_active_builds"`
	ValidateMoreMinScore         float64 `yaml:"validate_more_min_score"`
	WeakMarginFloor              float64 `yaml:"weak_margin_floor"`
	MaxVerdictAgeHours           int     `yaml:"max_verdict_age_hours"`
	ValidationCostCapUSD         float64 `yaml:"validation_cost_cap_usd"`
}

// Config is the fully-parsed scoring and threshold configuration. Version is
// recorded on every verdict so a decision can never be re-explained after the
// fact with different weights.
type Config struct {
	Version               string              `yaml:"version"`
	Weights               Weights             `yaml:"weights"`
	DimensionSources      map[string][]string `yaml:"dimension_sources"`
	LabelStrength         map[string]float64  `yaml:"label_strength"`
	Thresholds            Thresholds          `yaml:"thresholds"`
	AssumedBasis          map[string]string   `yaml:"assumed_basis"`
	ReferenceMVPBudgetUSD float64             `yaml:"reference_mvp_budget_usd"`
	PlatformPolicyMarkers []string            `yaml:"platform_policy_risk_markers"`
	ValuePropAIMarkers    []string            `yaml:"value_prop_ai_markers"`
}

// LoadConfig reads and validates the scoring/threshold config. An empty path
// falls back to DefaultConfigPath.
func LoadConfig(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("opportunity: read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("opportunity: decode config %s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("opportunity: config %s: %w", path, err)
	}
	return c, nil
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("version is required")
	}
	for _, dim := range dimensionNames {
		if _, ok := c.DimensionSources[dim]; !ok {
			return fmt.Errorf("dimension_sources missing dimension %q", dim)
		}
	}
	for _, l := range []Label{LabelObserved, LabelInferred, LabelAssumed, LabelUnresolved} {
		if _, ok := c.LabelStrength[string(l)]; !ok {
			return fmt.Errorf("label_strength missing label %q", l)
		}
	}
	if c.ReferenceMVPBudgetUSD <= 0 {
		return fmt.Errorf("reference_mvp_budget_usd must be > 0")
	}
	return nil
}

// strength returns the configured evidence strength for a label. An unknown
// label fails closed to zero (an unlabeled statement contributes no evidence).
func (c Config) strength(l Label) float64 {
	if s, ok := c.LabelStrength[string(l)]; ok {
		return s
	}
	return 0
}

// weightFor returns the configured weight for a canonical dimension name.
func (w Weights) weightFor(dim string) float64 {
	switch dim {
	case dimPainSeverity:
		return w.PainSeverity
	case dimWTPEvidence:
		return w.WTPEvidence
	case dimReachableDistribution:
		return w.ReachableDistribution
	case dimFounderFit:
		return w.FounderFit
	case dimSpeedToMVP:
		return w.SpeedToMVP
	case dimRecurringRevenue:
		return w.RecurringRevenue
	case dimDefensibility:
		return w.Defensibility
	default:
		return 0
	}
}

// sortedStrings returns a stably-sorted copy so serialized config-derived
// lists never depend on map iteration order (prompt-caching / determinism).
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
