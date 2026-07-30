package research

import (
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// Skeptic is a distinct role that re-reads a proposed claim set and emits
// *reject* candidates only. It can lower an opportunity's score (by adding
// risk claims that flag weakly-evidenced positives), never raise it: it never
// emits an Observed claim and never upgrades an existing claim.
//
// This mirrors venture-loop.md §"Phase B" ("the generator may not assign the
// final score to its own ideas"): a second, adversarial read is required.
type Skeptic struct{}

// highImpactPositiveKinds are the kinds whose weak evidence most threatens a
// build decision, so an unverified one earns a skeptic risk flag.
var highImpactPositiveKinds = map[opportunity.ClaimKind]bool{
	opportunity.KindProblem:      true,
	opportunity.KindWTP:          true,
	opportunity.KindDistribution: true,
}

// Review returns additional risk claims (reject candidates) for every
// high-impact positive claim that is only Assumed or Unresolved. The returned
// claims are always risk-kind and never Observed, so they can only lower a
// later score, never raise it.
func (Skeptic) Review(claims []opportunity.Claim) []opportunity.Claim {
	var out []opportunity.Claim
	for _, c := range claims {
		if c.Kind == opportunity.KindRisk {
			continue
		}
		if !highImpactPositiveKinds[c.Kind] {
			continue
		}
		if c.Label == opportunity.LabelAssumed || c.Label == opportunity.LabelUnresolved {
			out = append(out, opportunity.Claim{
				Kind:      opportunity.KindRisk,
				Text:      "skeptic: " + string(c.Kind) + " evidence is only " + string(c.Label) + " — treat as unproven",
				Label:     opportunity.LabelInferred,
				Basis:     "skeptic review of weak positive evidence",
				Untrusted: true,
			})
		}
	}
	return out
}
