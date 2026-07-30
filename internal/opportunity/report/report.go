package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// Input is everything the renderers need: the opportunity (with its evidence),
// the deterministic scorecard, the verdict and its unmet thresholds, the
// research cost and a fixed generation timestamp.
type Input struct {
	Opportunity     opportunity.Opportunity
	Scorecard       opportunity.Scorecard
	Verdict         opportunity.Verdict
	UnmetThresholds []string
	ResearchCostUSD float64
	GeneratedAt     time.Time
}

// Artifact is one rendered Phase-C file.
type Artifact struct {
	Name    string
	Content []byte
}

// renderers maps each artifact filename to its deterministic renderer.
var markdownRenderers = []struct {
	name string
	fn   func(Input) (string, error)
}{
	{FileMarket, renderMarket},
	{FileCustomerLang, renderCustomerLanguage},
	{FileCompetitors, renderCompetitors},
	{FilePricing, renderPricing},
	{FileDistribution, renderDistribution},
	{FileUnitEconomics, renderUnitEconomics},
	{FileRisks, renderRisks},
	{FileExperimentPlan, renderExperimentPlan},
}

// Render produces the full nine-artifact Phase-C set, sorted by filename, so
// the output is byte-identical across runs for identical input.
func Render(in Input) ([]Artifact, error) {
	out := make([]Artifact, 0, len(markdownRenderers)+1)
	for _, r := range markdownRenderers {
		s, err := r.fn(in)
		if err != nil {
			return nil, err
		}
		out = append(out, Artifact{Name: r.name, Content: []byte(s)})
	}
	vr, err := ValidationReport(in)
	if err != nil {
		return nil, err
	}
	out = append(out, Artifact{Name: FileValidationReport, Content: vr})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UnresolvedAssumption is one unresolved claim surfaced in the report.
type UnresolvedAssumption struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// ValidationReportDoc is the machine-readable summary emitted as
// VALIDATION-REPORT.json.
type ValidationReportDoc struct {
	OpportunityID         string                 `json:"opportunity_id"`
	Verdict               string                 `json:"verdict"`
	TotalScore            float64                `json:"total_score"`
	ConfigVersion         string                 `json:"config_version"`
	ScorecardDigest       string                 `json:"scorecard_digest"`
	UnmetThresholds       []string               `json:"unmet_thresholds"`
	UnresolvedAssumptions []UnresolvedAssumption `json:"unresolved_assumptions"`
	ResearchCostUSD       float64                `json:"research_cost_usd"`
	GeneratedAt           string                 `json:"generated_at"`
}

// ValidationReport renders VALIDATION-REPORT.json deterministically.
func ValidationReport(in Input) ([]byte, error) {
	digest, err := in.Scorecard.Digest()
	if err != nil {
		return nil, err
	}
	unmet := append([]string(nil), in.UnmetThresholds...)
	sort.Strings(unmet)
	if unmet == nil {
		unmet = []string{}
	}
	assumptions := []UnresolvedAssumption{}
	for _, c := range in.Opportunity.Claims {
		if c.Label == opportunity.LabelUnresolved {
			assumptions = append(assumptions, UnresolvedAssumption{Kind: string(c.Kind), Text: c.Text})
		}
	}
	doc := ValidationReportDoc{
		OpportunityID:         in.Opportunity.Idea.ID,
		Verdict:               string(in.Verdict),
		TotalScore:            in.Scorecard.Total,
		ConfigVersion:         in.Scorecard.ConfigVersion,
		ScorecardDigest:       digest,
		UnmetThresholds:       unmet,
		UnresolvedAssumptions: assumptions,
		ResearchCostUSD:       in.ResearchCostUSD,
		GeneratedAt:           in.GeneratedAt.UTC().Format(time.RFC3339),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("report: encode validation report: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteBundle renders the artifact set into dir and returns an evidence.Bundle
// describing it, so the same evidence.Store (and `make evidence-verify`) that
// covers task evidence covers opportunity evidence with no new verification
// path (Task 103 Step 3). dir must be empty or non-existent.
func WriteBundle(dir string, in Input) (evidence.Bundle, error) {
	arts, err := Render(in)
	if err != nil {
		return evidence.Bundle{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return evidence.Bundle{}, fmt.Errorf("report: create bundle dir %s: %w", dir, err)
	}
	refs := make([]evidence.ArtifactRef, 0, len(arts))
	for _, a := range arts {
		p := filepath.Join(dir, a.Name)
		if err := os.WriteFile(p, a.Content, 0o644); err != nil {
			return evidence.Bundle{}, fmt.Errorf("report: write %s: %w", a.Name, err)
		}
		sum := sha256.Sum256(a.Content)
		refs = append(refs, evidence.ArtifactRef{
			Path:   a.Name,
			SHA256: hex.EncodeToString(sum[:]),
			Bytes:  int64(len(a.Content)),
		})
	}
	m := evidence.Manifest{
		TaskID:    in.Opportunity.Idea.ID,
		Artifacts: refs,
		CreatedAt: in.GeneratedAt.UTC(),
	}
	return evidence.Bundle{Manifest: m, Dir: dir}, nil
}
