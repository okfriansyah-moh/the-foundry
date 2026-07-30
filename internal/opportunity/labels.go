package opportunity

import "strings"

// NormalizeClaim applies fail-closed labeling identical to internal/spec's
// PostPass (docs/PLAN.md Task 100 Step 2):
//
//   - an absent or invalid label becomes Unresolved;
//   - Assumed with an empty Basis has its basis filled from config, or is
//     downgraded to Unresolved when config supplies none;
//   - a claim with an empty SourceRef can never be Observed — it is
//     downgraded to Inferred, because an Observed claim must point at a source.
//
// Normalization is idempotent: NormalizeClaim(NormalizeClaim(c)) == NormalizeClaim(c).
func NormalizeClaim(c Claim, cfg Config) Claim {
	c.Text = strings.TrimSpace(c.Text)
	c.Basis = strings.TrimSpace(c.Basis)
	c.SourceRef = strings.TrimSpace(c.SourceRef)

	if !c.Label.Valid() {
		c.Label = LabelUnresolved
	}

	// A source-free claim can never be Observed.
	if c.Label == LabelObserved && c.SourceRef == "" {
		c.Label = LabelInferred
	}

	// Assumed requires a basis; fill from config or downgrade.
	if c.Label == LabelAssumed && c.Basis == "" {
		if b, ok := cfg.AssumedBasis[string(c.Kind)]; ok && strings.TrimSpace(b) != "" {
			c.Basis = b
		} else {
			c.Label = LabelUnresolved
		}
	}

	// A non-Assumed claim with no basis gets a synthesized marker so every
	// stored/rendered claim carries a basis (mirrors spec.normalizeRequirement).
	if c.Label != LabelAssumed && c.Basis == "" {
		c.Basis = "synthesized"
	}
	return c
}

// Normalize returns a copy of o with every claim normalized. The original is
// not mutated.
func Normalize(o Opportunity, cfg Config) Opportunity {
	out := o
	out.Claims = make([]Claim, len(o.Claims))
	for i, c := range o.Claims {
		out.Claims[i] = NormalizeClaim(c, cfg)
	}
	return out
}

// impactForKind maps a claim kind to a coarse risk impact so opportunity gaps
// feed Task 45's discrepancy machinery the same way spec gaps do.
func impactForKind(k ClaimKind) Impact {
	switch k {
	case KindWTP, KindProblem, KindDistribution, KindRisk:
		return ImpactHigh
	case KindMarket, KindCompetitor, KindFrequency:
		return ImpactMedium
	default:
		return ImpactLow
	}
}

// UnresolvedByImpact mirrors spec.UnresolvedByImpact: it counts the
// opportunity's Unresolved claims bucketed by the impact of their kind, so the
// discrepancy machinery can prioritize which gaps to resolve first. A claim
// set is normalized first so downgrades are reflected.
func UnresolvedByImpact(o Opportunity, cfg Config) map[Impact]int {
	out := map[Impact]int{}
	for _, c := range Normalize(o, cfg).Claims {
		if c.Label == LabelUnresolved {
			out[impactForKind(c.Kind)]++
		}
	}
	return out
}
