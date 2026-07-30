package notify

import (
	"fmt"
	"sort"
	"strings"
)

// OpportunityCycleSummary is one opportunity cycle's outcome for the digest.
type OpportunityCycleSummary struct {
	OpportunityID string
	Verdict       string // BUILD | VALIDATE-MORE | REJECT
	TotalScore    float64
}

// OpportunityDigest is the Phase-A daily-loop digest section (docs/PLAN.md
// Task 103 / OPP-04). It is a veto surface, never an approval surface: it
// reports outcomes and never asks anyone to approve anything (Constitution
// C11/C23).
type OpportunityDigest struct {
	Generated               int
	PassedEvidenceThreshold int
	SelectedForValidation   int
	ResearchCostUSD         float64
	Cycles                  []OpportunityCycleSummary
}

// requiresNoAction reports whether every cycle is a REJECT/VALIDATE-MORE (i.e.
// "build nothing" or "one more experiment") — the "No action required" case.
func (d OpportunityDigest) requiresNoAction() bool {
	for _, c := range d.Cycles {
		if strings.EqualFold(c.Verdict, "BUILD") {
			return false
		}
	}
	return true
}

// FormatOpportunityDigest renders the daily opportunity digest section. The
// output is deterministic (cycles are sorted by opportunity ID) and always
// non-blocking: it never requests an approval.
func FormatOpportunityDigest(d OpportunityDigest) string {
	var sb strings.Builder
	sb.WriteString("💡 *Daily Opportunity Loop*\n")
	sb.WriteString(fmt.Sprintf("%d generated / %d passed evidence threshold / %d selected for deep validation\n",
		d.Generated, d.PassedEvidenceThreshold, d.SelectedForValidation))
	sb.WriteString(fmt.Sprintf("research cost $%.2f\n", d.ResearchCostUSD))

	cycles := append([]OpportunityCycleSummary(nil), d.Cycles...)
	sort.Slice(cycles, func(i, j int) bool { return cycles[i].OpportunityID < cycles[j].OpportunityID })
	for _, c := range cycles {
		sb.WriteString(fmt.Sprintf("- %s: %s (score %.1f)\n", c.OpportunityID, c.Verdict, c.TotalScore))
	}

	if d.requiresNoAction() {
		sb.WriteString("No action required.\n")
	} else {
		// Even a BUILD cycle is a veto opportunity, not an approval request.
		sb.WriteString("Review before build — veto only, no approval requested.\n")
	}
	return sb.String()
}
