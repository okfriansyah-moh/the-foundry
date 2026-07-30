package notify

import (
	"strings"
	"testing"
)

func TestFormatOpportunityDigestNoActionForRejectValidate(t *testing.T) {
	d := OpportunityDigest{
		Generated:               10,
		PassedEvidenceThreshold: 6,
		SelectedForValidation:   3,
		ResearchCostUSD:         2.5,
		Cycles: []OpportunityCycleSummary{
			{OpportunityID: "opp-b", Verdict: "VALIDATE-MORE", TotalScore: 58},
			{OpportunityID: "opp-a", Verdict: "REJECT", TotalScore: 12},
		},
	}
	out := FormatOpportunityDigest(d)
	if !strings.Contains(out, "No action required.") {
		t.Fatalf("REJECT/VALIDATE-MORE digest must say No action required:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "approve") {
		t.Fatalf("digest must never request an approval:\n%s", out)
	}
	// Deterministic ordering: opp-a before opp-b.
	if strings.Index(out, "opp-a") > strings.Index(out, "opp-b") {
		t.Fatalf("cycles not sorted by id:\n%s", out)
	}
}

func TestFormatOpportunityDigestBuildIsVetoNotApproval(t *testing.T) {
	d := OpportunityDigest{
		Generated: 1,
		Cycles:    []OpportunityCycleSummary{{OpportunityID: "opp-x", Verdict: "BUILD", TotalScore: 90}},
	}
	out := FormatOpportunityDigest(d)
	if strings.Contains(out, "No action required.") {
		t.Fatalf("a BUILD cycle should surface a veto opportunity, not No action required:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "veto") {
		t.Fatalf("BUILD digest must be veto-only:\n%s", out)
	}
}
