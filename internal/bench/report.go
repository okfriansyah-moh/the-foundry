package bench

import (
	"fmt"
	"math"
	"strings"
	"text/tabwriter"
)

// Verdict is the comparison outcome for one metric or threshold.
type Verdict string

const (
	VerdictInsufficientData Verdict = "insufficient data"
	VerdictMet              Verdict = "met"
	VerdictNotMet           Verdict = "not met"
	VerdictBaselineOnly     Verdict = "baseline only"
)

// Cell is one metric comparison between arms.
type Cell struct {
	MetricID       MetricID
	ControlValue   *float64
	ControlBasis   Basis
	ControlProxy   bool
	FoundryValue   *float64
	FoundryBasis   Basis
	FoundryProxy   bool
	Verdict        Verdict
	DeltaPercent   *float64
	Note           string
}

// ComparisonReport aggregates per-metric cells and quality-guard evaluation.
type ComparisonReport struct {
	ControlRuns  int
	FoundryRuns  int
	Cells        []Cell
	QualityGuard QualityGuardResult
	Overall      Verdict
}

// QualityGuardResult operationalizes "quality no worse than baseline".
type QualityGuardResult struct {
	Evaluated bool
	Passed    bool
	Notes     []string
}

// Compare builds a per-metric comparison table from control and foundry runs.
func Compare(control, foundry []*RunRecord) ComparisonReport {
	defs := AllMetrics()
	cells := make([]Cell, 0, len(defs))
	for _, d := range defs {
		cMean, cBasis, cProxy, cN := aggregateMetric(control, d.ID)
		fMean, fBasis, fProxy, fN := aggregateMetric(foundry, d.ID)
		cell := Cell{
			MetricID:     d.ID,
			ControlValue: cMean,
			ControlBasis: cBasis,
			ControlProxy: cProxy,
			FoundryValue: fMean,
			FoundryBasis: fBasis,
			FoundryProxy: fProxy,
		}
		switch {
		case len(control) == 0 && len(foundry) == 0:
			cell.Verdict = VerdictInsufficientData
			cell.Note = "no runs in either arm"
		case len(foundry) == 0:
			cell.Verdict = VerdictBaselineOnly
			if cN == 0 {
				cell.Verdict = VerdictInsufficientData
				cell.Note = "no measurable control observations"
			} else {
				cell.Note = "foundry arm not recorded yet (Task 135)"
			}
		case cN == 0 || fN == 0:
			cell.Verdict = VerdictInsufficientData
			cell.Note = "metric lacks comparable observations in both arms"
		default:
			if cMean != nil && fMean != nil && *cMean != 0 {
				delta := ((*fMean - *cMean) / *cMean) * 100
				cell.DeltaPercent = &delta
			}
			cell.Verdict = VerdictMet
			cell.Note = "comparison available — threshold evaluation deferred to Task 135"
		}
		cells = append(cells, cell)
	}
	qg := evaluateQualityGuard(control, foundry)
	overall := VerdictBaselineOnly
	if len(foundry) > 0 {
		overall = VerdictInsufficientData
		for _, c := range cells {
			if c.Verdict == VerdictMet || c.Verdict == VerdictNotMet {
				overall = VerdictMet
				break
			}
		}
	}
	return ComparisonReport{
		ControlRuns:  len(control),
		FoundryRuns:  len(foundry),
		Cells:        cells,
		QualityGuard: qg,
		Overall:      overall,
	}
}

func aggregateMetric(runs []*RunRecord, id MetricID) (*float64, Basis, bool, int) {
	var sum float64
	var n int
	basis := BasisNotMeasurable
	proxy := false
	for _, r := range runs {
		o, ok := r.ObservationFor(id)
		if !ok || o.Value == nil || o.Basis == BasisNotMeasurable {
			continue
		}
		sum += *o.Value
		n++
		basis = o.Basis
		if o.Proxy {
			proxy = true
		}
	}
	if n == 0 {
		return nil, BasisNotMeasurable, false, 0
	}
	mean := sum / float64(n)
	mean = math.Round(mean*1000) / 1000
	return &mean, basis, proxy, n
}

func evaluateQualityGuard(control, foundry []*RunRecord) QualityGuardResult {
	res := QualityGuardResult{Evaluated: len(control) >= 3}
	if !res.Evaluated {
		res.Notes = append(res.Notes, "need ≥3 control runs to establish baseline quality guard")
		return res
	}
	if len(foundry) == 0 {
		res.Notes = append(res.Notes, "foundry arm absent — quality guard baseline recorded, gate pending Task 135")
		res.Passed = true
		return res
	}
	cDef, _, _, cN := aggregateMetric(control, MetricDefectsAfterHandoff)
	fDef, _, _, fN := aggregateMetric(foundry, MetricDefectsAfterHandoff)
	if cN == 0 || fN == 0 {
		res.Notes = append(res.Notes, "defects_after_handoff insufficient data in both arms")
		return res
	}
	if fDef != nil && cDef != nil && *fDef <= *cDef {
		res.Passed = true
		res.Notes = append(res.Notes, fmt.Sprintf("defects after handoff: foundry %.2f ≤ control %.2f", *fDef, *cDef))
	} else if fDef != nil && cDef != nil {
		res.Notes = append(res.Notes, fmt.Sprintf("defects after handoff: foundry %.2f > control %.2f — quality gate would fail", *fDef, *cDef))
	}
	cEv, _, _, cEvN := aggregateMetric(control, MetricEvidenceRejectionRate)
	fEv, _, _, fEvN := aggregateMetric(foundry, MetricEvidenceRejectionRate)
	if cEvN > 0 && fEvN > 0 && fEv != nil && cEv != nil {
		if *fEv <= *cEv {
			res.Notes = append(res.Notes, fmt.Sprintf("evidence rejection rate: foundry %.3f ≤ control %.3f", *fEv, *cEv))
		} else {
			res.Passed = false
			res.Notes = append(res.Notes, fmt.Sprintf("evidence rejection rate: foundry %.3f > control %.3f", *fEv, *cEv))
		}
	}
	return res
}

// RenderMarkdown formats the comparison report as a markdown table.
func RenderMarkdown(report ComparisonReport, targets Targets) string {
	var b strings.Builder
	b.WriteString("# V1 acceleration benchmark report\n\n")
	b.WriteString(fmt.Sprintf("**Threshold config:** %s (%s)\n\n", targets.Label, targets.Version))
	b.WriteString(fmt.Sprintf("Control runs: %d · Foundry runs: %d · Overall: **%s**\n\n", report.ControlRuns, report.FoundryRuns, report.Overall))
	b.WriteString("## Per-metric comparison\n\n")
	b.WriteString("| Metric | Control | Control basis | Foundry | Foundry basis | Verdict | Notes |\n")
	b.WriteString("| --- | ---: | --- | ---: | --- | --- | --- |\n")
	for _, c := range report.Cells {
		def, _ := DefinitionByID(c.MetricID)
		b.WriteString(fmt.Sprintf("| %s | %s | %s%s | %s | %s%s | %s | %s |\n",
			def.Label,
			formatValue(c.ControlValue, def.Unit),
			c.ControlBasis, proxySuffix(c.ControlProxy),
			formatValue(c.FoundryValue, def.Unit),
			c.FoundryBasis, proxySuffix(c.FoundryProxy),
			c.Verdict,
			escapePipe(c.Note),
		))
	}
	b.WriteString("\n## Quality guard\n\n")
	if report.QualityGuard.Evaluated {
		status := "pending"
		if report.QualityGuard.Passed {
			status = "baseline recorded"
		} else if report.FoundryRuns > 0 {
			status = "would fail"
		}
		b.WriteString(fmt.Sprintf("Status: **%s**\n\n", status))
	} else {
		b.WriteString("Status: **insufficient data**\n\n")
	}
	for _, n := range report.QualityGuard.Notes {
		b.WriteString(fmt.Sprintf("- %s\n", n))
	}
	b.WriteString("\n## V1 acceptance targets (not universal claims)\n\n")
	b.WriteString("### Personal path\n\n")
	b.WriteString(fmt.Sprintf("- Manual orchestration reduction: ≥%.0f%%\n", targets.Personal.ManualOrchestrationReduction*100))
	b.WriteString(fmt.Sprintf("- Delivery lead time reduction: ≥%.0f%%\n", targets.Personal.DeliveryLeadTimeReduction*100))
	b.WriteString(fmt.Sprintf("- Unauthorized actions: ≤%.0f\n", targets.Personal.UnauthorizedActionsMax))
	b.WriteString("\n### 10x path\n\n")
	b.WriteString(fmt.Sprintf("- PLAN → handoff reduction: ≥%.0f%%\n", targets.TenX.PlanToHandoffReduction*100))
	b.WriteString(fmt.Sprintf("- Coordination/reporting reduction: ≥%.0f%%\n", targets.TenX.CoordinationReportingReduction*100))
	b.WriteString(fmt.Sprintf("- Unauthorized SCM operations: ≤%.0f\n", targets.TenX.UnauthorizedSCMOperationsMax))
	b.WriteString(fmt.Sprintf("\n_%s_\n", targets.Quality.Description))
	return b.String()
}

// RenderText formats a plain-text table (for CLI output).
func RenderText(report ComparisonReport) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "METRIC\tCONTROL\tBASIS\tFOUNDRY\tBASIS\tVERDICT")
	for _, c := range report.Cells {
		def, _ := DefinitionByID(c.MetricID)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			def.ID,
			formatValue(c.ControlValue, def.Unit),
			c.ControlBasis,
			formatValue(c.FoundryValue, def.Unit),
			c.FoundryBasis,
			c.Verdict,
		)
	}
	_ = w.Flush()
	return b.String()
}

func formatValue(v *float64, unit Unit) string {
	if v == nil {
		return "—"
	}
	switch unit {
	case UnitRatio:
		return fmt.Sprintf("%.3f", *v)
	case UnitUSD, UnitUSDPerTask:
		return fmt.Sprintf("%.2f", *v)
	default:
		return fmt.Sprintf("%.2f", *v)
	}
}

func proxySuffix(proxy bool) string {
	if proxy {
		return " (proxy)"
	}
	return ""
}

func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
