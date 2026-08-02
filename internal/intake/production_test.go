package intake_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
)

type fixedValidator struct{ out intake.ValidateOutput }

func (f fixedValidator) Validate(context.Context, intake.ValidateInput) (intake.ValidateOutput, error) {
	return f.out, nil
}

type allowSignal struct{}

func (allowSignal) HasAllowlistedRealSignal(context.Context, string) (bool, error) { return true, nil }

type denySignal struct{}

func (denySignal) HasAllowlistedRealSignal(context.Context, string) (bool, error) { return false, nil }

func TestSignalBackedValidator_MissingSignalStopsBUILD(t *testing.T) {
	v := intake.SignalBackedValidator{
		Inner:      fixedValidator{out: intake.ValidateOutput{Verdict: "BUILD", Digest: "d"}},
		RealSignal: denySignal{},
		OpportunityIDForIdea: func(string) string { return "opp-1" },
	}
	out, err := v.Validate(context.Background(), intake.ValidateInput{Idea: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict != "VALIDATE-MORE" {
		t.Fatalf("verdict = %s", out.Verdict)
	}
}

func TestSignalBackedValidator_AllowsBUILDWithRealSignal(t *testing.T) {
	v := intake.SignalBackedValidator{
		Inner:      fixedValidator{out: intake.ValidateOutput{Verdict: "BUILD", Digest: "d"}},
		RealSignal: allowSignal{},
		OpportunityIDForIdea: func(string) string { return "opp-1" },
	}
	out, err := v.Validate(context.Background(), intake.ValidateInput{Idea: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict != "BUILD" {
		t.Fatalf("verdict = %s", out.Verdict)
	}
}
