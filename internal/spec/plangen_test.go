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
	raw, err := PlanFromSpecification("plan-spec-test", "Generated Plan", specIn, mapping)
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
	raw, err := PlanFromSpecification("plan-spec-rows", "Rows", specIn, mapping)
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
			got, err := PlanFromSpecification(tc.planID, tc.title, specIn, mapping)
			if err != nil {
				t.Fatalf("PlanFromSpecification: %v", err)
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
