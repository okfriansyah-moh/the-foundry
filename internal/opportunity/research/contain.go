package research

import (
	"context"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// Refusal records a claim that containment dropped and why, so a refusal is
// auditable rather than silent.
type Refusal struct {
	Claim  opportunity.Claim
	Reason string
}

// Refusal reasons.
const (
	ReasonInjection = "injection-imperative-addressed-to-system"
)

// builtinInjectionMarkers are always active regardless of config, so a config
// that forgets a marker cannot open the containment boundary.
var builtinInjectionMarkers = []string{
	"ignore previous instructions",
	"ignore all previous",
	"disregard previous",
	"you are now",
	"system prompt",
	"</system>",
	"<system>",
}

// Contain enforces the untrusted-content boundary on research-produced claims
// (docs/PLAN.md Task 101 Step 2):
//
//   - a claim whose text contains an imperative addressed to the system is
//     refused (dropped) and recorded — it never becomes evidence;
//   - every surviving claim is marked Untrusted;
//   - a surviving claim may be labeled at most Inferred unless its SourceRef
//     resolves to a hash-verified stored artifact. A fabricated or
//     unresolvable SourceRef downgrades an Observed claim to Inferred and
//     clears the (unverified) ref so it cannot masquerade as provenance.
func Contain(ctx context.Context, claims []opportunity.Claim, cfg Config, resolver ArtifactResolver) (kept []opportunity.Claim, refused []Refusal) {
	if resolver == nil {
		resolver = noArtifacts{}
	}
	markers := append(append([]string(nil), builtinInjectionMarkers...), cfg.InjectionMarkers...)

	for _, c := range claims {
		if looksLikeInjection(c.Text, markers) {
			refused = append(refused, Refusal{Claim: c, Reason: ReasonInjection})
			continue
		}

		c.Untrusted = true

		verified := false
		if strings.TrimSpace(c.SourceRef) != "" {
			if _, ok := resolver.Resolve(ctx, c.SourceRef); ok {
				verified = true
			}
		}

		if !verified {
			// Cap at Inferred and strip the unverified ref: an unresolvable
			// SourceRef can never anchor an Observed label.
			if c.Label == opportunity.LabelObserved {
				c.Label = opportunity.LabelInferred
			}
			c.SourceRef = ""
		}

		kept = append(kept, c)
	}
	return kept, refused
}

// looksLikeInjection reports whether text contains any injection marker
// (case-insensitive substring). Markers cover the imperative-addressed-to-the-
// system shape that fetched web content and model output may smuggle in.
func looksLikeInjection(text string, markers []string) bool {
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
