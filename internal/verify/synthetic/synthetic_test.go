package synthetic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeCheck struct {
	name string
	err  error
}

func (f fakeCheck) Name() string              { return f.name }
func (f fakeCheck) Run(context.Context) error { return f.err }

func TestDecideVerificationMode_AndPhrasing(t *testing.T) {
	policy := CanarySignalPolicy{MinSessions: 100, MinTransactions: 20}
	mode := DecideVerificationMode(policy, TrafficSample{Sessions: 5, Transactions: 0})
	if mode != ModeSyntheticSubstitute {
		t.Fatalf("mode=%s, want %s", mode, ModeSyntheticSubstitute)
	}
	phrase := ModeNotification(mode)
	if !strings.Contains(phrase, "synthetic — not real user validation") {
		t.Fatalf("phrase=%q missing required C21 wording", phrase)
	}
}

func TestRunBattery_FailureBlocks(t *testing.T) {
	out := RunBattery(context.Background(), ModeHybrid, []Check{
		fakeCheck{name: "playwright-smoke"},
		fakeCheck{name: "billing-flow", err: errors.New("webhook mismatch")},
	})
	if out.Passed() {
		t.Fatal("expected battery failure")
	}
}
