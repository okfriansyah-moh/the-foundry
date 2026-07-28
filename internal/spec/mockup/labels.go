package mockup

import (
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

type Stage string

const (
	StageScreenComponents Stage = "screen-components"
	StageUserFlow         Stage = "user-flow"
	StageInteraction      Stage = "interaction-state"
	StageA11y             Stage = "a11y"
	StageBackendInference Stage = "backend-data-api-inference"
)

// NormalizeLabel applies deterministic caps:
// 1) inference stage outputs can never be Observed
// 2) low-confidence outputs cannot claim Observed.
func NormalizeLabel(stage Stage, confidence float64, suggested spec.Label) spec.Label {
	lbl := suggested
	if !lbl.Valid() {
		lbl = spec.LabelUnresolved
	}
	if stage == StageBackendInference && lbl == spec.LabelObserved {
		lbl = spec.LabelInferred
	}
	if confidence < 0.85 && lbl == spec.LabelObserved {
		lbl = spec.LabelInferred
	}
	return lbl
}

func HighImpactUnresolved(text string, label spec.Label) bool {
	if label != spec.LabelUnresolved {
		return false
	}
	t := strings.ToLower(text)
	return strings.Contains(t, "auth") ||
		strings.Contains(t, "payment") ||
		strings.Contains(t, "billing") ||
		strings.Contains(t, "delete")
}
