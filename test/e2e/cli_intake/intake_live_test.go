package cliintake_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
)

func TestSignalGateRefuseBUILDWithoutRealSignal(t *testing.T) {
	v := intake.SignalBackedValidator{
		Inner:                fixed{out: intake.ValidateOutput{Verdict: "BUILD"}},
		RealSignal:           deny{},
		OpportunityIDForIdea: func(string) string { return "opp" },
	}
	out, err := v.Validate(context.Background(), intake.ValidateInput{Idea: "idea"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict != "VALIDATE-MORE" {
		t.Fatalf("got %s", out.Verdict)
	}
}

type fixed struct{ out intake.ValidateOutput }

func (f fixed) Validate(context.Context, intake.ValidateInput) (intake.ValidateOutput, error) {
	return f.out, nil
}

type deny struct{}

func (deny) HasAllowlistedRealSignal(context.Context, string) (bool, error) { return false, nil }
