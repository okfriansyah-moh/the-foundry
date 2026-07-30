// Package report renders an opportunity's evidence into the Phase-C artifact
// set (docs/PLAN.md Task 103 / OPP-04) and the machine-readable
// VALIDATION-REPORT.json, deterministically and with every claim carrying its
// label and source reference inline. An unlabeled claim is a render error, not
// a silent default.
//
// Boundary: read/render only. This package makes no verdict, no state
// transition and no approval request — the Telegram digest it feeds is a veto
// surface, never an approval surface (Constitution C11/C23).
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// The nine Phase-C artifact filenames (venture-loop.md §"Phase C").
const (
	FileMarket           = "MARKET.md"
	FileCustomerLang     = "CUSTOMER-LANGUAGE.md"
	FileCompetitors      = "COMPETITORS.md"
	FilePricing          = "PRICING.md"
	FileDistribution     = "DISTRIBUTION.md"
	FileUnitEconomics    = "UNIT-ECONOMICS.md"
	FileRisks            = "RISKS.md"
	FileExperimentPlan   = "experiment-plan.yaml"
	FileValidationReport = "VALIDATION-REPORT.json"
)

// renderClaim renders one claim with its label and source reference inline. An
// invalid/empty label is a render error — no default is applied here (Task 103
// Step 1).
func renderClaim(c opportunity.Claim) (string, error) {
	if !c.Label.Valid() {
		return "", fmt.Errorf("report: claim %q of kind %q has no valid label", c.Text, c.Kind)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- **[%s]** %s", c.Label, strings.TrimSpace(c.Text))
	if u := c.Untrusted; u {
		b.WriteString(" _(untrusted)_")
	}
	b.WriteString("\n")
	if ref := strings.TrimSpace(c.SourceRef); ref != "" {
		fmt.Fprintf(&b, "  - source: %s\n", ref)
	}
	if basis := strings.TrimSpace(c.Basis); basis != "" {
		fmt.Fprintf(&b, "  - basis: %s\n", basis)
	}
	return b.String(), nil
}

// renderSection renders a titled markdown section listing every claim of the
// given kinds, in claim order. Empty sections state so explicitly rather than
// rendering nothing.
func renderSection(title, intro string, claims []opportunity.Claim, kinds ...opportunity.ClaimKind) (string, error) {
	want := map[opportunity.ClaimKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if intro != "" {
		fmt.Fprintf(&b, "%s\n\n", intro)
	}
	n := 0
	for _, c := range claims {
		if !want[c.Kind] {
			continue
		}
		s, err := renderClaim(c)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
		n++
	}
	if n == 0 {
		b.WriteString("_No evidence recorded for this section._\n")
	}
	return b.String(), nil
}

// renderMarket renders MARKET.md.
func renderMarket(in Input) (string, error) {
	return renderSection("Market", "Market-size and demand evidence.", in.Opportunity.Claims, opportunity.KindMarket)
}

// renderCustomerLanguage renders CUSTOMER-LANGUAGE.md from problem/frequency
// claims (the customer's own words about the pain).
func renderCustomerLanguage(in Input) (string, error) {
	return renderSection("Customer Language", "How customers describe the problem and how often they hit it.", in.Opportunity.Claims, opportunity.KindProblem, opportunity.KindFrequency)
}

func renderCompetitors(in Input) (string, error) {
	return renderSection("Competitors", "Competitors and existing alternatives/workarounds.", in.Opportunity.Claims, opportunity.KindCompetitor, opportunity.KindAlternative)
}

func renderPricing(in Input) (string, error) {
	return renderSection("Pricing", "Willingness-to-pay evidence and pricing hypothesis.", in.Opportunity.Claims, opportunity.KindWTP)
}

// renderDistribution renders DISTRIBUTION.md from distribution claims plus the
// ICP's reachable channels.
func renderDistribution(in Input) (string, error) {
	sec, err := renderSection("Distribution", "Reachable distribution channels.", in.Opportunity.Claims, opportunity.KindDistribution)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(sec)
	b.WriteString("\n## Reachable channels (ICP)\n\n")
	if len(in.Opportunity.ICP.ReachableChannels) == 0 {
		b.WriteString("_No reachable channels recorded._\n")
	}
	for _, ch := range in.Opportunity.ICP.ReachableChannels {
		reach := "not reachable"
		if ch.Reachable {
			reach = "reachable"
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", ch.Name, strings.TrimSpace(ch.Kind), reach)
	}
	return b.String(), nil
}

// renderUnitEconomics renders UNIT-ECONOMICS.md from the economic envelope and
// recurring-revenue evidence.
func renderUnitEconomics(in Input) (string, error) {
	var b strings.Builder
	b.WriteString("# Unit Economics\n\n")
	fmt.Fprintf(&b, "- Estimated validation cost: $%.2f\n", in.Opportunity.EstimatedValidationCostUSD)
	fmt.Fprintf(&b, "- MVP budget: $%.2f\n", in.Opportunity.MVPBudgetUSD)
	fmt.Fprintf(&b, "- Max active builds: %d\n\n", in.Opportunity.MaxActiveBuilds)
	b.WriteString("## Recurring-revenue evidence\n\n")
	n := 0
	for _, c := range in.Opportunity.Claims {
		if c.Kind != opportunity.KindWTP {
			continue
		}
		s, err := renderClaim(c)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
		n++
	}
	if n == 0 {
		b.WriteString("_No recurring-revenue evidence recorded._\n")
	}
	return b.String(), nil
}

// renderRisks renders RISKS.md from risk claims and every Unresolved claim.
func renderRisks(in Input) (string, error) {
	var b strings.Builder
	b.WriteString("# Risks\n\n")
	n := 0
	for _, c := range in.Opportunity.Claims {
		if c.Kind != opportunity.KindRisk {
			continue
		}
		s, err := renderClaim(c)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
		n++
	}
	if n == 0 {
		b.WriteString("_No explicit risk claims recorded._\n")
	}
	b.WriteString("\n## Unresolved assumptions\n\n")
	m := 0
	for _, c := range in.Opportunity.Claims {
		if c.Label != opportunity.LabelUnresolved {
			continue
		}
		fmt.Fprintf(&b, "- (%s) %s\n", c.Kind, strings.TrimSpace(c.Text))
		m++
	}
	if m == 0 {
		b.WriteString("_No unresolved assumptions._\n")
	}
	return b.String(), nil
}

// renderExperimentPlan renders experiment-plan.yaml: the operator's bounded
// Phase-C validation work, meaningful chiefly for VALIDATE-MORE outcomes.
func renderExperimentPlan(in Input) (string, error) {
	var b strings.Builder
	b.WriteString("# experiment-plan.yaml\n")
	b.WriteString("# docs/PLAN.md Task 103 (OPP-04): operator-executed validation work.\n")
	fmt.Fprintf(&b, "verdict: %s\n", in.Verdict)
	fmt.Fprintf(&b, "validation_budget_usd: %.2f\n", in.Opportunity.EstimatedValidationCostUSD)
	b.WriteString("experiments:\n")
	unmet := append([]string(nil), in.UnmetThresholds...)
	sort.Strings(unmet)
	if len(unmet) == 0 {
		b.WriteString("  - name: none\n    goal: no unmet thresholds to close\n")
	}
	for i, u := range unmet {
		fmt.Fprintf(&b, "  - name: close-%d\n    goal: gather evidence to satisfy %q\n", i+1, u)
	}
	return b.String(), nil
}
