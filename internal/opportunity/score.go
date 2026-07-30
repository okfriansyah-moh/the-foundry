package opportunity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Canonical dimension names. dimensionNames is kept sorted so every
// serialization of a Scorecard is byte-stable regardless of map iteration
// order (docs/PLAN.md Task 100 acceptance: byte-identical Scorecard).
const (
	dimDefensibility         = "defensibility"
	dimFounderFit            = "founder_fit"
	dimPainSeverity          = "pain_severity"
	dimReachableDistribution = "reachable_distribution"
	dimRecurringRevenue      = "recurring_revenue"
	dimSpeedToMVP            = "speed_to_mvp"
	dimWTPEvidence           = "wtp_evidence"
)

// dimensionNames is the sorted, canonical iteration order for scoring.
var dimensionNames = []string{
	dimDefensibility,
	dimFounderFit,
	dimPainSeverity,
	dimReachableDistribution,
	dimRecurringRevenue,
	dimSpeedToMVP,
	dimWTPEvidence,
}

// DimensionScore is one Phase-B dimension's contribution to the total.
type DimensionScore struct {
	Name     string  `json:"name"`
	Weight   float64 `json:"weight"`
	Subscore float64 `json:"subscore"` // 0..100
	Reason   string  `json:"reason"`
}

// Signals carries the derived facts Decide needs for its named reject rules,
// so Decide can depend only on a Scorecard (its stated signature) rather than
// re-reading the raw Opportunity.
type Signals struct {
	MaxLabelStrength     float64 `json:"max_label_strength"`
	StrongEvidenceFloor  float64 `json:"strong_evidence_floor"`
	HasAnyEvidence       bool    `json:"has_any_evidence"`
	HasReachableChannel  bool    `json:"has_reachable_channel"`
	ValuePropOnlyAI      bool    `json:"value_prop_only_ai"`
	WeakGrossMargin      bool    `json:"weak_gross_margin"`
	PlatformPolicyRisk   bool    `json:"platform_policy_risk"`
	RealValidationSignal bool    `json:"real_validation_signal"`
	MVPBudgetUSD         float64 `json:"mvp_budget_usd"`
	ActiveBuildsCap      int     `json:"active_builds_cap"`
	PaymentEvidenceScore float64 `json:"payment_evidence_score"`
	DistributionScore    float64 `json:"distribution_score"`
}

// Scorecard is the deterministic output of Score. Its Canonical() encoding is
// byte-identical for identical input.
type Scorecard struct {
	ConfigVersion string           `json:"config_version"`
	Dimensions    []DimensionScore `json:"dimensions"`
	RiskPenalty   float64          `json:"risk_penalty"`
	CostPenalty   float64          `json:"cost_penalty"`
	Total         float64          `json:"total"`
	Signals       Signals          `json:"signals"`
}

// Dimension returns the named dimension score, if present.
func (s Scorecard) Dimension(name string) (DimensionScore, bool) {
	for _, d := range s.Dimensions {
		if d.Name == name {
			return d, true
		}
	}
	return DimensionScore{}, false
}

// round2 rounds to two decimals so float noise never breaks byte-identity
// while still preserving meaningful precision.
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// Score computes the deterministic Scorecard for an opportunity under a given
// config. It is a pure function: identical (o, cfg) yields a byte-identical
// Scorecard. The opportunity is normalized first so label downgrades are
// reflected in the score.
func Score(o Opportunity, cfg Config) Scorecard {
	n := Normalize(o, cfg)

	dims := make([]DimensionScore, 0, len(dimensionNames))
	weightedPositive := 0.0
	for _, dim := range dimensionNames {
		sources := cfg.DimensionSources[dim]
		sub, reason := dimensionSubscore(n.Claims, sources, cfg)
		w := cfg.Weights.weightFor(dim)
		dims = append(dims, DimensionScore{
			Name:     dim,
			Weight:   round2(w),
			Subscore: round2(sub),
			Reason:   reason,
		})
		weightedPositive += w * sub
	}

	riskPenalty := penaltyRisk(n.Claims, cfg)
	costPenalty := penaltyCost(n, cfg)

	total := weightedPositive - riskPenalty - costPenalty
	total = clamp(total, 0, 100)

	sc := Scorecard{
		ConfigVersion: cfg.Version,
		Dimensions:    dims,
		RiskPenalty:   round2(riskPenalty),
		CostPenalty:   round2(costPenalty),
		Total:         round2(total),
		Signals:       signals(n, dims, cfg),
	}
	return sc
}

// dimensionSubscore averages the label strength of the claims whose kind is in
// sources, scaled to 0..100. A dimension with no contributing evidence scores
// 0 — which is what guarantees an all-Assumed/all-Unresolved opportunity can
// never reach a high total.
func dimensionSubscore(claims []Claim, sources []string, cfg Config) (float64, string) {
	sources = sortedStrings(sources)
	kindSet := map[string]bool{}
	for _, s := range sources {
		kindSet[s] = true
	}
	var sum float64
	var count int
	labelCounts := map[Label]int{}
	for _, c := range claims {
		if !kindSet[string(c.Kind)] {
			continue
		}
		sum += cfg.strength(c.Label)
		count++
		labelCounts[c.Label]++
	}
	if count == 0 {
		return 0, "no " + strings.Join(sources, "/") + " evidence"
	}
	sub := (sum / float64(count)) * 100
	return sub, fmt.Sprintf("%d %s claim(s): %s", count, strings.Join(sources, "/"), labelBreakdown(labelCounts))
}

func labelBreakdown(counts map[Label]int) string {
	parts := make([]string, 0, 4)
	for _, l := range []Label{LabelObserved, LabelInferred, LabelAssumed, LabelUnresolved} {
		if counts[l] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[l], l))
		}
	}
	return strings.Join(parts, ", ")
}

// penaltyRisk scales the average strength of risk claims by the risk-penalty
// weight: a well-evidenced (Observed) risk penalizes more than an assumed one.
func penaltyRisk(claims []Claim, cfg Config) float64 {
	var sum float64
	var count int
	for _, c := range claims {
		if c.Kind == KindRisk {
			sum += cfg.strength(c.Label)
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return cfg.Weights.RiskPenalty * (sum / float64(count)) * 100
}

// penaltyCost scales the validation cost (capped at the reference budget) by
// the cost-penalty weight.
func penaltyCost(o Opportunity, cfg Config) float64 {
	if cfg.ReferenceMVPBudgetUSD <= 0 {
		return 0
	}
	ratio := o.EstimatedValidationCostUSD / cfg.ReferenceMVPBudgetUSD
	ratio = clamp(ratio, 0, 1)
	return cfg.Weights.CostPenalty * ratio * 100
}

func signals(o Opportunity, dims []DimensionScore, cfg Config) Signals {
	sig := Signals{
		StrongEvidenceFloor:  cfg.strength(LabelInferred),
		RealValidationSignal: o.RealValidationSignal,
		MVPBudgetUSD:         o.MVPBudgetUSD,
		ActiveBuildsCap:      o.MaxActiveBuilds,
	}
	for _, c := range o.Claims {
		st := cfg.strength(c.Label)
		if st > sig.MaxLabelStrength {
			sig.MaxLabelStrength = st
		}
		if c.Label != LabelUnresolved {
			sig.HasAnyEvidence = true
		}
		if c.Kind == KindRisk && matchesAny(c.Text, cfg.PlatformPolicyMarkers) {
			sig.PlatformPolicyRisk = true
		}
	}
	sig.MaxLabelStrength = round2(sig.MaxLabelStrength)
	for _, ch := range o.ICP.ReachableChannels {
		if ch.Reachable {
			sig.HasReachableChannel = true
			break
		}
	}
	if d, ok := dimensionByName(dims, dimWTPEvidence); ok {
		sig.PaymentEvidenceScore = d.Subscore
	}
	if d, ok := dimensionByName(dims, dimReachableDistribution); ok {
		sig.DistributionScore = d.Subscore
	}
	if d, ok := dimensionByName(dims, dimRecurringRevenue); ok {
		sig.WeakGrossMargin = d.Subscore < cfg.Thresholds.WeakMarginFloor
	}
	sig.ValuePropOnlyAI = valuePropOnlyAI(o, cfg)
	return sig
}

func dimensionByName(dims []DimensionScore, name string) (DimensionScore, bool) {
	for _, d := range dims {
		if d.Name == name {
			return d, true
		}
	}
	return DimensionScore{}, false
}

// valuePropOnlyAI flags the Phase-A reject rule "value proposition that is only
// 'uses AI'": the statement leans on an AI marker while there is no
// problem/WTP evidence to anchor a real value proposition.
func valuePropOnlyAI(o Opportunity, cfg Config) bool {
	if !matchesAny(o.Idea.Statement, cfg.ValuePropAIMarkers) {
		return false
	}
	for _, c := range o.Claims {
		if c.Label == LabelUnresolved {
			continue
		}
		if c.Kind == KindProblem || c.Kind == KindWTP {
			return false
		}
	}
	return true
}

func matchesAny(text string, markers []string) bool {
	lt := strings.ToLower(text)
	for _, m := range markers {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if strings.Contains(lt, m) {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Canonical returns a byte-stable JSON encoding of the scorecard: keys are
// emitted in Go struct order and dimensions are already sorted by name, so
// identical input produces identical bytes.
func (s Scorecard) Canonical() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("opportunity: canonicalize scorecard: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Digest returns the hex sha256 of the canonical scorecard encoding.
func (s Scorecard) Digest() (string, error) {
	b, err := s.Canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
