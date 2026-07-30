package report_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity/report"
)

const oppConfigPath = "../../../config/opportunity-thresholds.yaml"

func fixtureInput(t *testing.T) report.Input {
	t.Helper()
	cfg, err := opportunity.LoadConfig(oppConfigPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	opp := opportunity.Opportunity{
		Idea: opportunity.Idea{ID: "opp-report-1", Statement: "A scheduling tool for solo consultants."},
		ICP: opportunity.ICP{
			Segment:           "solo consultants",
			EconomicBuyer:     "the consultant",
			ReachableChannels: []opportunity.Channel{{Name: "consultant communities", Kind: "community", Reachable: true}},
		},
		Claims: []opportunity.Claim{
			{Kind: opportunity.KindProblem, Text: "Consultants lose hours to booking back-and-forth.", Label: opportunity.LabelObserved, SourceRef: "interview://c1#h1", Basis: "12 interviews"},
			{Kind: opportunity.KindFrequency, Text: "The pain recurs weekly.", Label: opportunity.LabelInferred, SourceRef: "interview://c1#h2"},
			{Kind: opportunity.KindWTP, Text: "Most said they would pay $20/mo.", Label: opportunity.LabelInferred, SourceRef: "interview://c1#h3"},
			{Kind: opportunity.KindDistribution, Text: "Two communities allow tool posts.", Label: opportunity.LabelInferred, SourceRef: "channel://a#h4"},
			{Kind: opportunity.KindMarket, Text: "~1.2M solo consultants in the geography.", Label: opportunity.LabelAssumed, Basis: "industry census"},
			{Kind: opportunity.KindCompetitor, Text: "Incumbents target teams, not solos.", Label: opportunity.LabelInferred, SourceRef: "review://g2#h5"},
			{Kind: opportunity.KindRisk, Text: "Adoption depends on community goodwill.", Label: opportunity.LabelUnresolved},
		},
		EstimatedValidationCostUSD: 40,
		MVPBudgetUSD:               130,
		MaxActiveBuilds:            1,
		RealValidationSignal:       false,
	}
	sc := opportunity.Score(opp, cfg)
	verdict, unmet := opportunity.Decide(sc, cfg.Thresholds)
	return report.Input{
		Opportunity:     opp,
		Scorecard:       sc,
		Verdict:         verdict,
		UnmetThresholds: unmet,
		ResearchCostUSD: 1.25,
		GeneratedAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func TestRenderGoldens(t *testing.T) {
	in := fixtureInput(t)
	arts, err := report.Render(in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(arts) != 9 {
		t.Fatalf("expected 9 artifacts, got %d", len(arts))
	}
	update := os.Getenv("UPDATE_GOLDENS") == "1"
	for _, a := range arts {
		golden := filepath.Join("testdata", "goldens", a.Name)
		if update {
			if err := os.WriteFile(golden, a.Content, 0o644); err != nil {
				t.Fatalf("write golden %s: %v", a.Name, err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s (run with UPDATE_GOLDENS=1 to create): %v", a.Name, err)
		}
		if string(want) != string(a.Content) {
			t.Fatalf("artifact %s does not match golden\n--- got ---\n%s\n--- want ---\n%s", a.Name, a.Content, want)
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	in := fixtureInput(t)
	first, err := report.Render(in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := report.Render(in)
		if err != nil {
			t.Fatalf("render iter %d: %v", i, err)
		}
		for j := range got {
			if got[j].Name != first[j].Name || string(got[j].Content) != string(first[j].Content) {
				t.Fatalf("non-deterministic render at %s", got[j].Name)
			}
		}
	}
}

func TestUnlabeledClaimIsRenderError(t *testing.T) {
	in := fixtureInput(t)
	in.Opportunity.Claims = append(in.Opportunity.Claims, opportunity.Claim{Kind: opportunity.KindMarket, Text: "no label", Label: opportunity.Label("bogus")})
	if _, err := report.Render(in); err == nil {
		t.Fatalf("an unlabeled claim must be a render error, not a default")
	}
}

func TestBundleVerifies(t *testing.T) {
	in := fixtureInput(t)
	srcDir := t.TempDir()
	bundle, err := report.WriteBundle(srcDir, in)
	if err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if len(bundle.Manifest.Artifacts) != 9 {
		t.Fatalf("expected 9 artifacts in manifest, got %d", len(bundle.Manifest.Artifacts))
	}
	store := evidence.NewFSStore(t.TempDir())
	id, err := store.Put(bundle)
	if err != nil {
		t.Fatalf("put bundle: %v", err)
	}
	if err := store.Verify(id); err != nil {
		t.Fatalf("evidence verify failed: %v", err)
	}
}
