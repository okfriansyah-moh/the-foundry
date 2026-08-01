package mockup

import (
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// itemBasis records how an extracted item was obtained. NodeRef carries
// format-specific refs (figma:, html:, pdf:); vision-sourced items fall back
// to vision:<stage> (docs/PLAN.md Task 138).
func itemBasis(item ExtractedItem) string {
	if item.NodeRef != "" {
		return item.NodeRef
	}
	return "vision:" + string(item.Stage)
}

// BuildExtraction normalizes labels and seeds requirements from extracted items.
// idPrefix is the requirement ID stem ("mockup" for vision/HTML/PDF routes,
// "figma" for Figma structural ingestion).
func BuildExtraction(idPrefix string, items []ExtractedItem) Extraction {
	reqs := make([]spec.Requirement, 0, len(items))
	high := make([]spec.Requirement, 0)
	normalized := make([]ExtractedItem, 0, len(items))
	for i, item := range items {
		item.Label = NormalizeLabel(item.Stage, item.Confidence, item.Label)
		normalized = append(normalized, item)
		req := spec.Requirement{
			ID:      fmt.Sprintf("%s-%d", idPrefix, i+1),
			Section: item.Section,
			Text:    item.Text,
			Label:   item.Label,
			Basis:   itemBasis(item),
			Impact:  spec.ImpactMedium,
		}
		if HighImpactUnresolved(item.Text, item.Label) {
			req.Impact = spec.ImpactHigh
			high = append(high, req)
		}
		reqs = append(reqs, req)
	}
	return Extraction{
		Items:                normalized,
		HighImpactUnresolved: high,
		SeedRequirements:     reqs,
	}
}
