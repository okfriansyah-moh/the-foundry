package admission_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// goldenCase is one fixture plan and its expected outcome, covering every
// RulesV1 entry individually plus rule combinations and the self-
// classification hard gate (docs/PLAN.md Task 7 Step 5).
type goldenCase struct {
	name        string
	wantTier    admission.Tier
	wantErr     error
	wantRuleIDs []string
}

var goldenCases = []goldenCase{
	{name: "docs-only", wantTier: admission.TierA0, wantRuleIDs: []string{"docs-copy-tests"}},
	{name: "code-only", wantTier: admission.TierA0, wantRuleIDs: []string{"docs-copy-tests"}},
	{name: "dependency-only", wantTier: admission.TierA1, wantRuleIDs: []string{"dependency-migration-network-secret-deploy"}},
	{name: "migration-only", wantTier: admission.TierA1, wantRuleIDs: []string{"dependency-migration-network-secret-deploy"}},
	{name: "network-only", wantTier: admission.TierA1, wantRuleIDs: []string{"dependency-migration-network-secret-deploy"}},
	{name: "secret-only", wantTier: admission.TierA1, wantRuleIDs: []string{"dependency-migration-network-secret-deploy"}},
	{name: "deploy-nonprod", wantTier: admission.TierA1, wantRuleIDs: []string{"dependency-migration-network-secret-deploy"}},
	{name: "billing-only", wantTier: admission.TierH, wantRuleIDs: []string{"billing-permission-destructive"}},
	{name: "permission-only", wantTier: admission.TierH, wantRuleIDs: []string{"billing-permission-destructive"}},
	{name: "destructive-only", wantTier: admission.TierH, wantRuleIDs: []string{"billing-permission-destructive"}},
	{
		name:        "deploy-production",
		wantTier:    admission.TierA2,
		wantRuleIDs: []string{"dependency-migration-network-secret-deploy", "production-deploy"},
	},
	{
		name:     "combo-docs-dependency-deploy-production",
		wantTier: admission.TierA2,
		wantRuleIDs: []string{
			"dependency-migration-network-secret-deploy",
			"docs-copy-tests",
			"production-deploy",
		},
	},
	{
		name:        "combo-code-billing",
		wantTier:    admission.TierH,
		wantRuleIDs: []string{"billing-permission-destructive", "docs-copy-tests"},
	},
	{
		name:     "self-classified",
		wantTier: admission.TierH,
		wantErr:  admission.ErrSelfClassification,
	},
}

func loadFixture(t *testing.T, name string) *plan.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "plans", name+".md"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return doc
}

// TestGolden runs each fixture plan through Classify 5 times, asserts the
// marshaled Decision is byte-identical across all 5 runs, and compares the
// result against the committed golden JSON (docs/PLAN.md Task 7 Step 5 /
// Acceptance).
func TestGolden(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			doc := loadFixture(t, tc.name)
			policy := admission.NoopPolicyView{}

			var first []byte
			for i := 0; i < 5; i++ {
				decision, err := admission.Classify(doc, policy)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("run %d: err = %v, want %v", i, err, tc.wantErr)
				}
				if decision.Tier != tc.wantTier {
					t.Fatalf("run %d: tier = %v, want %v", i, decision.Tier, tc.wantTier)
				}
				if tc.wantErr == nil && !equalStrings(decision.RulesEvaluated, tc.wantRuleIDs) {
					t.Fatalf("run %d: rules evaluated = %v, want %v", i, decision.RulesEvaluated, tc.wantRuleIDs)
				}

				got, err := json.MarshalIndent(decision, "", "  ")
				if err != nil {
					t.Fatalf("run %d: marshal decision: %v", i, err)
				}
				if i == 0 {
					first = got
					continue
				}
				if string(got) != string(first) {
					t.Fatalf("run %d: marshaled Decision diverged from run 0:\nrun0=%s\nrun%d=%s", i, first, i, got)
				}
			}

			goldenPath := filepath.Join("testdata", "golden", tc.name+".json")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", goldenPath, err)
			}
			if string(first) != string(want) {
				t.Fatalf("marshaled Decision does not match golden %s:\ngot=%s\nwant=%s", goldenPath, first, want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSelfClassificationBeforeRuleset verifies the hard gate short-circuits
// before any RulesV1 entry is evaluated: RulesEvaluated must stay empty (the
// zero value), never populated by rules that would otherwise have fired on
// the fixture's declared effects.
func TestSelfClassificationBeforeRuleset(t *testing.T) {
	doc := loadFixture(t, "self-classified")
	decision, err := admission.Classify(doc, admission.NoopPolicyView{})
	if !errors.Is(err, admission.ErrSelfClassification) {
		t.Fatalf("err = %v, want ErrSelfClassification", err)
	}
	if decision.Tier != admission.TierH {
		t.Fatalf("tier = %v, want TierH", decision.Tier)
	}
	if len(decision.RulesEvaluated) != 0 {
		t.Fatalf("RulesEvaluated = %v, want empty: ruleset must not run after the hard gate", decision.RulesEvaluated)
	}
	if decision.ClassifierVersion != "" {
		t.Fatalf("ClassifierVersion = %q, want empty on the hard-gate short-circuit path", decision.ClassifierVersion)
	}
}

// TestDeterministicAcrossPolicyOrdering pins down that RequiredControls is
// sorted in the output regardless of the order PolicyView returns them in,
// so no map/slice iteration order can leak into the persisted Decision.
func TestDeterministicAcrossPolicyOrdering(t *testing.T) {
	doc := loadFixture(t, "billing-only")
	policy := fakePolicy{digest: "sha256:deadbeef", controls: []string{"human-approval", "budget-check", "audit-log"}}

	decision, err := admission.Classify(doc, policy)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	want := []string{"audit-log", "budget-check", "human-approval"}
	if !equalStrings(decision.RequiredControls, want) {
		t.Fatalf("RequiredControls = %v, want sorted %v", decision.RequiredControls, want)
	}
	if decision.PolicyDigest != "sha256:deadbeef" {
		t.Fatalf("PolicyDigest = %q, want %q", decision.PolicyDigest, "sha256:deadbeef")
	}
}

type fakePolicy struct {
	digest   string
	controls []string
}

func (f fakePolicy) Digest() string { return f.digest }

func (f fakePolicy) RequiredControls(admission.Tier) []string { return f.controls }
