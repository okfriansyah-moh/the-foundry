package spec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostPass_AdversarialOutputEnforced(t *testing.T) {
	defaults, err := LoadDefaults("../../config/spec-defaults.yaml")
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	spec := PostPass([]Requirement{
		{ID: "r1", Section: "billing", Text: "collect payment", Label: LabelAssumed, Basis: ""},
		{ID: "r2", Section: "permissions", Text: "admins only", Label: Label("invalid"), Basis: ""},
	}, defaults)

	for _, r := range spec.Requirements {
		if !r.Label.Valid() {
			t.Fatalf("requirement %s has invalid label %q after postpass", r.ID, r.Label)
		}
		if r.Label == LabelAssumed && strings.TrimSpace(r.Basis) == "" {
			t.Fatalf("assumed requirement %s has empty basis after postpass", r.ID)
		}
	}
	for _, section := range completenessSections {
		if len(spec.BySection[section]) == 0 {
			t.Fatalf("section %q missing after postpass completeness enforcement", section)
		}
	}
}

func TestSynthesize_GoldensStableFromReplayCassettes(t *testing.T) {
	defaults, err := LoadDefaults("../../config/spec-defaults.yaml")
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	cases := []struct {
		cassette string
		title    string
		golden   string
	}{
		{cassette: "../../test/cassettes/spec/req1.json", title: "Spec Fixture 1", golden: "testdata/goldens/spec_req1.md"},
		{cassette: "../../test/cassettes/spec/req2.json", title: "Spec Fixture 2", golden: "testdata/goldens/spec_req2.md"},
		{cassette: "../../test/cassettes/spec/req3.json", title: "Spec Fixture 3", golden: "testdata/goldens/spec_req3.md"},
	}

	for _, tc := range cases {
		t.Run(filepath.Base(tc.cassette), func(t *testing.T) {
			replay, err := LoadReplaySource(tc.cassette)
			if err != nil {
				t.Fatalf("LoadReplaySource: %v", err)
			}
			syn := Synthesizer{Source: replay, Defaults: defaults}
			spec, err := syn.Synthesize(context.Background(), "input does not affect replay mode")
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			got := RenderMarkdown(tc.title, spec)
			wantRaw, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			want := string(wantRaw)
			if got != want {
				t.Fatalf("golden mismatch for %s", tc.golden)
			}
		})
	}
}
