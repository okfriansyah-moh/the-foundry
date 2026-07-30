package spec

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

func TestPlanGen_StrictParseAndNoSelfClassification(t *testing.T) {
	mapping, err := loadEffectMapping("../../config/effect-mapping.yaml")
	if err != nil {
		t.Fatalf("loadEffectMapping: %v", err)
	}
	specIn := Specification{
		Requirements: []Requirement{
			{ID: "r1", Section: "apis", Text: "API endpoint", Label: LabelObserved},
			{ID: "r2", Section: "billing", Text: "billing flow", Label: LabelUnresolved},
		},
		Sections: []string{"apis", "billing"},
	}
	raw, err := PlanFromSpecification("plan-spec-test", "Generated Plan", specIn, mapping, testMC())
	if err != nil {
		t.Fatalf("PlanFromSpecification: %v", err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if doc.SelfClassified {
		t.Fatal("SelfClassified=true; generated plan must never set declared tier")
	}
	// Least-privilege: no requested permission may target a wildcard.
	for _, p := range doc.RequestedPermissions {
		if p.Target == "*" {
			t.Fatalf("generated plan requested a wildcard permission: %+v", p)
		}
	}
}

func testMC() MissionContext {
	return MissionContext{
		RepoAlias:       "product",
		RepoURL:         "https://github.com/example/mission-repo",
		RepoBranch:      "main",
		BudgetUSD:       75,
		RepoWriteTarget: "product/**",
	}
}

// TestPlanGen_NoWildcardPermission is the anti-wildcard regression: a generated
// plan must never request repo-write to "*" (the pre-Task-110 hardcoded hole).
func TestPlanGen_NoWildcardPermission(t *testing.T) {
	mapping, err := loadEffectMapping("../../config/effect-mapping.yaml")
	if err != nil {
		t.Fatalf("loadEffectMapping: %v", err)
	}
	specIn := Specification{Sections: []string{"apis", "billing"}}
	raw, err := PlanFromSpecification("plan-nowild", "No Wildcard", specIn, mapping, testMC())
	if err != nil {
		t.Fatalf("PlanFromSpecification: %v", err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	sawRepoWrite := false
	for _, p := range doc.RequestedPermissions {
		if p.Target == "*" {
			t.Fatalf("wildcard permission emitted: %+v", p)
		}
		if p.Kind == "repo-write" {
			sawRepoWrite = true
			if p.Target != "product/**" {
				t.Fatalf("repo-write must target the mission path, got %q", p.Target)
			}
		}
	}
	if !sawRepoWrite {
		t.Fatal("expected a scoped repo-write permission")
	}
}

// TestPlanGen_WildcardTargetRejected proves a wildcard repo-write target is a
// generation error, not silently emitted.
func TestPlanGen_WildcardTargetRejected(t *testing.T) {
	mapping := EffectMapping{}
	specIn := Specification{Sections: []string{"apis"}}
	mc := testMC()
	mc.RepoWriteTarget = "*"
	if _, err := PlanFromSpecification("plan-bad", "Bad", specIn, mapping, mc); err == nil {
		t.Fatal("a wildcard repo-write target must be rejected")
	}
}

func TestPlanGen_EffectMappingRowsCovered(t *testing.T) {
	mapping, err := loadEffectMapping("../../config/effect-mapping.yaml")
	if err != nil {
		t.Fatalf("loadEffectMapping: %v", err)
	}
	specIn := Specification{
		Requirements: nil,
		Sections:     []string{"apis", "billing", "persistence", "analytics", "permissions", "authentication"},
	}
	raw, err := PlanFromSpecification("plan-spec-rows", "Rows", specIn, mapping, testMC())
	if err != nil {
		t.Fatalf("PlanFromSpecification: %v", err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	wantRows := len(mapping.Rows)
	if len(doc.DeclaredEffects) != wantRows {
		t.Fatalf("declared effects count = %d, want %d", len(doc.DeclaredEffects), wantRows)
	}
}

func TestPlanGen_GoldensStable(t *testing.T) {
	mapping, err := loadEffectMapping("../../config/effect-mapping.yaml")
	if err != nil {
		t.Fatalf("loadEffectMapping: %v", err)
	}
	cases := []struct {
		specFile string
		planID   string
		title    string
		golden   string
	}{
		{specFile: "testdata/plangen/spec1.yaml", planID: "generated-spec-1", title: "Generated Spec 1", golden: "testdata/goldens/plangen_spec1.md"},
		{specFile: "testdata/plangen/spec2.yaml", planID: "generated-spec-2", title: "Generated Spec 2", golden: "testdata/goldens/plangen_spec2.md"},
		{specFile: "testdata/plangen/spec3.yaml", planID: "generated-spec-3", title: "Generated Spec 3", golden: "testdata/goldens/plangen_spec3.md"},
	}
	for _, tc := range cases {
		t.Run(filepath.Base(tc.specFile), func(t *testing.T) {
			specIn, err := loadSpecFixture(tc.specFile)
			if err != nil {
				t.Fatalf("loadSpecFixture: %v", err)
			}
			got, err := PlanFromSpecification(tc.planID, tc.title, specIn, mapping, testMC())
			if err != nil {
				t.Fatalf("PlanFromSpecification: %v", err)
			}
			if os.Getenv("UPDATE_GOLDENS") == "1" {
				if err := os.WriteFile(tc.golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("golden mismatch for %s", tc.golden)
			}
		})
	}
}

func loadEffectMapping(path string) (EffectMapping, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return EffectMapping{}, err
	}
	var m EffectMapping
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return EffectMapping{}, err
	}
	return m, nil
}

func loadSpecFixture(path string) (Specification, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Specification{}, err
	}
	var in struct {
		Sections []string `yaml:"sections"`
	}
	if err := yaml.Unmarshal(raw, &in); err != nil {
		return Specification{}, err
	}
	return Specification{Sections: in.Sections, Requirements: []Requirement{{ID: "fixture", Section: in.Sections[0], Text: "fixture", Label: LabelObserved}}}, nil
}
