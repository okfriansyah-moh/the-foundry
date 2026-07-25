package main

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		risk string
		rev  string
		want Tier
	}{
		{"low r1 auto", "Low", "R1", Auto},
		{"med r2 auto", "Med", "R2", Auto},
		{"medium r2 auto", "Medium", "R2", Auto},
		{"low r3 gated by rev", "Low", "R3", Gated},
		{"high r1 gated by risk", "High", "R1", Gated},
		{"high r3 gated both", "High", "R3", Gated},
		{"high r4 gated both", "High", "R4", Gated},
		{"bolded rev value", "Low", "**R1**", Auto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := Classify(tc.risk, tc.rev)
			if got != tc.want {
				t.Errorf("Classify(%q, %q) = %v, want %v", tc.risk, tc.rev, got, tc.want)
			}
		})
	}
}

func TestClassifyGatedReasonNamesTheField(t *testing.T) {
	_, reason := Classify("High", "R3")
	if reason == "" {
		t.Fatalf("GATED classification must carry a non-empty reason for the Telegram message")
	}
}
