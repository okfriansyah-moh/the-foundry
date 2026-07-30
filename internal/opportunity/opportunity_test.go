package opportunity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const configPath = "../../config/opportunity-thresholds.yaml"

func testConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func loadFixture(t *testing.T, name string) Opportunity {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var o Opportunity
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return o
}

func evaluate(t *testing.T, cfg Config, o Opportunity) (Scorecard, Verdict, []string) {
	t.Helper()
	sc := Score(o, cfg)
	v, blockers := Decide(sc, cfg.Thresholds)
	return sc, v, blockers
}

func TestGoldenCorpusVerdicts(t *testing.T) {
	cfg := testConfig(t)
	cases := []struct {
		file      string
		want      Verdict
		mustNotBe Verdict
	}{
		{file: "build.json", want: VerdictBuild},
		{file: "validate_more.json", want: VerdictValidateMore},
		{file: "reject.json", want: VerdictReject},
		{file: "all_assumed.json", mustNotBe: VerdictBuild},
		{file: "no_payment_evidence.json", mustNotBe: VerdictBuild},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			o := loadFixture(t, tc.file)
			sc, v, blockers := evaluate(t, cfg, o)
			if tc.want != "" && v != tc.want {
				t.Fatalf("%s: got verdict %q (total=%.2f, blockers=%v), want %q", tc.file, v, sc.Total, blockers, tc.want)
			}
			if tc.mustNotBe != "" && v == tc.mustNotBe {
				t.Fatalf("%s: verdict must never be %q (total=%.2f)", tc.file, tc.mustNotBe, sc.Total)
			}
		})
	}
}

// TestScoreDeterministic asserts byte-identical scorecards across 1000
// iterations for identical input (docs/PLAN.md Task 100 acceptance).
func TestScoreDeterministic(t *testing.T) {
	cfg := testConfig(t)
	o := loadFixture(t, "build.json")
	first, err := Score(o, cfg).Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	firstDigest, err := Score(o, cfg).Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	for i := 0; i < 1000; i++ {
		got, err := Score(o, cfg).Canonical()
		if err != nil {
			t.Fatalf("iter %d canonical: %v", i, err)
		}
		if string(got) != string(first) {
			t.Fatalf("iter %d: non-deterministic scorecard\nfirst: %s\ngot:   %s", i, first, got)
		}
		d, err := Score(o, cfg).Digest()
		if err != nil {
			t.Fatalf("iter %d digest: %v", i, err)
		}
		if d != firstDigest {
			t.Fatalf("iter %d: non-deterministic digest %s != %s", i, d, firstDigest)
		}
	}
}

// TestAllAssumedNeverBuildsRegardlessOfTotal proves the evidence-strength
// guard: even a maxed total can never BUILD when the best evidence is Assumed.
func TestAllAssumedNeverBuildsRegardlessOfTotal(t *testing.T) {
	cfg := testConfig(t)
	sc := Scorecard{
		ConfigVersion: cfg.Version,
		Total:         100,
		Signals: Signals{
			MaxLabelStrength:     cfg.strength(LabelAssumed),
			StrongEvidenceFloor:  cfg.strength(LabelInferred),
			HasAnyEvidence:       true,
			HasReachableChannel:  true,
			RealValidationSignal: true,
			MVPBudgetUSD:         100,
			PaymentEvidenceScore: 100,
			DistributionScore:    100,
		},
	}
	v, blockers := Decide(sc, cfg.Thresholds)
	if v == VerdictBuild {
		t.Fatalf("all-assumed scorecard with total=100 must never BUILD; got %q", v)
	}
	if !contains(blockers, RejectAllAssumed) {
		t.Fatalf("expected %q blocker, got %v", RejectAllAssumed, blockers)
	}
}

// passingScorecard returns a scorecard that Decide accepts as BUILD.
func passingScorecard(cfg Config) Scorecard {
	return Scorecard{
		ConfigVersion: cfg.Version,
		Total:         90,
		Signals: Signals{
			MaxLabelStrength:     cfg.strength(LabelObserved),
			StrongEvidenceFloor:  cfg.strength(LabelInferred),
			HasAnyEvidence:       true,
			HasReachableChannel:  true,
			RealValidationSignal: true,
			MVPBudgetUSD:         100,
			PaymentEvidenceScore: 90,
			DistributionScore:    90,
		},
	}
}

func TestEachNumericThresholdBlocksBuild(t *testing.T) {
	cfg := testConfig(t)
	th := cfg.Thresholds

	// Baseline must BUILD, else the per-threshold assertions prove nothing.
	if v, b := Decide(passingScorecard(cfg), th); v != VerdictBuild {
		t.Fatalf("baseline scorecard must BUILD, got %q (blockers=%v)", v, b)
	}

	cases := []struct {
		name    string
		mutate  func(*Scorecard)
		blocker string
	}{
		{"minimum_total_score", func(s *Scorecard) { s.Total = th.MinimumTotalScore - 1 }, UnmetTotalScore},
		{"minimum_distribution_score", func(s *Scorecard) { s.Signals.DistributionScore = th.MinimumDistributionScore - 1 }, UnmetDistributionScore},
		{"minimum_payment_evidence_score", func(s *Scorecard) { s.Signals.PaymentEvidenceScore = th.MinimumPaymentEvidenceScore - 1 }, UnmetPaymentEvidence},
		{"must_have_real_validation_signal", func(s *Scorecard) { s.Signals.RealValidationSignal = false }, UnmetRealValidation},
		{"maximum_mvp_budget_usd", func(s *Scorecard) { s.Signals.MVPBudgetUSD = th.MaximumMVPBudgetUSD + 1 }, UnmetMVPBudget},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sc := passingScorecard(cfg)
			tc.mutate(&sc)
			v, blockers := Decide(sc, th)
			if v == VerdictBuild {
				t.Fatalf("threshold %s should block BUILD", tc.name)
			}
			if !contains(blockers, tc.blocker) {
				t.Fatalf("expected blocker %q, got %v", tc.blocker, blockers)
			}
		})
	}
}

// TestNoPaymentEvidenceFailsOnPaymentAlone proves the fixture clears the total
// threshold yet is blocked solely by the payment-evidence threshold.
func TestNoPaymentEvidenceFailsOnPaymentAlone(t *testing.T) {
	cfg := testConfig(t)
	o := loadFixture(t, "no_payment_evidence.json")
	sc, v, blockers := evaluate(t, cfg, o)
	if v == VerdictBuild {
		t.Fatalf("no-payment fixture must not BUILD")
	}
	if sc.Total < cfg.Thresholds.MinimumTotalScore {
		t.Fatalf("fixture must clear the total threshold to prove payment blocks alone; total=%.2f", sc.Total)
	}
	if !contains(blockers, UnmetPaymentEvidence) {
		t.Fatalf("expected %q blocker, got %v", UnmetPaymentEvidence, blockers)
	}
	for _, b := range blockers {
		if b != UnmetPaymentEvidence {
			t.Fatalf("expected payment-evidence to be the only blocker, also saw %q", b)
		}
	}
}

func TestEmptySourceRefNeverObserved(t *testing.T) {
	cfg := testConfig(t)
	c := Claim{Kind: KindProblem, Text: "x", Label: LabelObserved, SourceRef: ""}
	got := NormalizeClaim(c, cfg)
	if got.Label == LabelObserved {
		t.Fatalf("a claim with empty SourceRef must never be Observed; got %q", got.Label)
	}
	if got.Label != LabelInferred {
		t.Fatalf("expected downgrade to Inferred, got %q", got.Label)
	}
	// With a source ref it stays Observed.
	c.SourceRef = "src://1#hash"
	if got := NormalizeClaim(c, cfg); got.Label != LabelObserved {
		t.Fatalf("Observed with a source ref must survive; got %q", got.Label)
	}
}

func TestAssumedEmptyBasisDowngradesOrFills(t *testing.T) {
	cfg := testConfig(t)
	// Known kind => basis filled from config.
	c := Claim{Kind: KindProblem, Text: "x", Label: LabelAssumed}
	got := NormalizeClaim(c, cfg)
	if got.Label != LabelAssumed || got.Basis == "" {
		t.Fatalf("Assumed with a config basis should stay Assumed with a filled basis; got label=%q basis=%q", got.Label, got.Basis)
	}
	// Unknown kind with no config basis => downgraded to Unresolved.
	cfg2 := cfg
	cfg2.AssumedBasis = map[string]string{}
	if got := NormalizeClaim(c, cfg2); got.Label != LabelUnresolved {
		t.Fatalf("Assumed with no basis and no config default must downgrade to Unresolved; got %q", got.Label)
	}
}

// TestConfigDrivenOutcome proves weights/thresholds change the outcome with no
// code change: raising the minimum total flips a BUILD to not-BUILD.
func TestConfigDrivenOutcome(t *testing.T) {
	cfg := testConfig(t)
	o := loadFixture(t, "build.json")
	if _, v, _ := evaluate(t, cfg, o); v != VerdictBuild {
		t.Fatalf("baseline must BUILD, got %q", v)
	}
	strict := cfg
	strict.Thresholds.MinimumTotalScore = 99.9
	sc := Score(o, strict)
	if v, _ := Decide(sc, strict.Thresholds); v == VerdictBuild {
		t.Fatalf("raising minimum_total_score in config must block BUILD with no code change")
	}
}

func TestRejectDefaultWhenUnevaluable(t *testing.T) {
	cfg := testConfig(t)
	// An empty scorecard (no evidence, no reachable channel) must REJECT,
	// never BUILD.
	v, _ := Decide(Scorecard{}, cfg.Thresholds)
	if v == VerdictBuild {
		t.Fatalf("an unevaluable scorecard must never BUILD; got %q", v)
	}
	if v != VerdictReject {
		t.Fatalf("an unevaluable scorecard must REJECT; got %q", v)
	}
}

func TestUnresolvedByImpact(t *testing.T) {
	cfg := testConfig(t)
	o := Opportunity{
		Claims: []Claim{
			{Kind: KindWTP, Text: "a", Label: LabelUnresolved},         // high
			{Kind: KindMarket, Text: "b", Label: LabelUnresolved},      // medium
			{Kind: KindAlternative, Text: "c", Label: LabelUnresolved}, // low
			{Kind: KindProblem, Text: "d", Label: LabelObserved, SourceRef: "s://1"},
		},
	}
	got := UnresolvedByImpact(o, cfg)
	if got[ImpactHigh] != 1 || got[ImpactMedium] != 1 || got[ImpactLow] != 1 {
		t.Fatalf("unexpected impact buckets: %+v", got)
	}
}

func TestValuePropOnlyAIRejected(t *testing.T) {
	cfg := testConfig(t)
	o := loadFixture(t, "reject.json")
	_, v, blockers := evaluate(t, cfg, o)
	if v != VerdictReject {
		t.Fatalf("AI-only value prop must REJECT, got %q", v)
	}
	if !contains(blockers, RejectValuePropOnlyAI) {
		t.Fatalf("expected %q blocker, got %v", RejectValuePropOnlyAI, blockers)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
