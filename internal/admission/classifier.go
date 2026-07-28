package admission

import (
	"fmt"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/admission/detect"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// NoopPolicyView is a zero-behavior PolicyView stub: empty policy digest, no
// required controls for any tier. It stands in for real policy-store
// integration, which is out of scope for this task.
type NoopPolicyView struct{}

// Digest implements PolicyView.
func (NoopPolicyView) Digest() string { return "" }

// RequiredControls implements PolicyView.
func (NoopPolicyView) RequiredControls(Tier) []string { return nil }

// riskScoreFor maps a tier to a fixed, deterministic risk score. v0 has no
// finer-grained scoring input than the tier itself; Task 45's detected
// effects and discrepancies are expected to refine this.
func riskScoreFor(t Tier) float64 {
	switch t {
	case TierA0:
		return 0.0
	case TierA1:
		return 0.3
	case TierA2:
		return 0.6
	case TierH:
		return 1.0
	default:
		return 1.0
	}
}

// Classify computes the admission Decision for doc against policy.
//
// Classify is a pure function: same doc and policy answers always produce a
// byte-identical marshaled Decision. It performs no I/O, reads no clock, and
// draws no randomness.
//
// The self-classification hard gate runs before any ruleset evaluation
// (Constitution C6): if doc declared its own tier, Classify returns
// ErrSelfClassification and a Decision carrying only Tier: TierH and a
// fixed Explanation — the caller is expected to map this to
// FAILED/ADMISSION_REJECTED and must not fall through to treat any other
// field of the returned Decision as authoritative.
func Classify(doc *plan.Document, policy PolicyView) (Decision, error) {
	if doc == nil {
		return Decision{}, fmt.Errorf("admission: nil document")
	}
	if policy == nil {
		policy = NoopPolicyView{}
	}

	if doc.SelfClassified {
		return Decision{
			Tier:        TierH,
			Explanation: "plan-authored tier ignored",
		}, ErrSelfClassification
	}

	declared := append([]plan.Effect{}, doc.DeclaredEffects...)
	detected := detect.FromDocument(doc)
	discrepancies := discrepancyEffects(declared, detected)

	union := make([]plan.Effect, 0, len(declared)+len(detected))
	union = append(union, declared...)
	union = append(union, detected...)

	fired := make(map[string]struct{})
	tier := TierA0
	for _, rule := range RulesV1_1 {
		for _, e := range union {
			if rule.Match(e) {
				fired[rule.ID] = struct{}{}
				if rule.TierFloor > tier {
					tier = rule.TierFloor
				}
				break
			}
		}
	}

	rulesEvaluated := make([]string, 0, len(fired))
	for id := range fired {
		rulesEvaluated = append(rulesEvaluated, id)
	}
	sort.Strings(rulesEvaluated)

	tier = applyDiscrepancyFloor(tier, discrepancies)

	requiredControls := append([]string{}, policy.RequiredControls(tier)...)
	sort.Strings(requiredControls)

	return Decision{
		ClassifierVersion: Version,
		PolicyDigest:      policy.Digest(),
		RulesEvaluated:    rulesEvaluated,
		Declared:          declared,
		Detected:          detected,
		Discrepancies:     discrepancies,
		RiskScore:         riskScoreFor(tier),
		Tier:              tier,
		RequiredControls:  requiredControls,
		Explanation:       explanationFor(tier, rulesEvaluated),
	}, nil
}

func discrepancyEffects(declared, detected []plan.Effect) []plan.Effect {
	declaredSet := map[string]struct{}{}
	for _, e := range declared {
		declaredSet[string(e.Kind)+"|"+e.Target] = struct{}{}
	}
	out := make([]plan.Effect, 0)
	for _, e := range detected {
		key := string(e.Kind) + "|" + e.Target
		if _, ok := declaredSet[key]; ok {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Target < out[j].Target
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func applyDiscrepancyFloor(base Tier, discrepancies []plan.Effect) Tier {
	if len(discrepancies) == 0 {
		return base
	}
	for _, e := range discrepancies {
		if e.Kind == plan.EffectBilling || e.Kind == plan.EffectSecret || e.Kind == plan.EffectDestructive {
			return TierH
		}
	}
	switch base {
	case TierA0:
		return TierA1
	case TierA1:
		return TierA2
	case TierA2:
		return TierH
	default:
		return TierH
	}
}

// explanationFor renders a deterministic, human-readable summary of why a
// tier was assigned.
func explanationFor(tier Tier, rulesEvaluated []string) string {
	if len(rulesEvaluated) == 0 {
		return fmt.Sprintf("tier %s: no rules fired, default floor applies", tier)
	}
	return fmt.Sprintf("tier %s: highest floor among fired rules [%s]", tier, strings.Join(rulesEvaluated, ", "))
}
