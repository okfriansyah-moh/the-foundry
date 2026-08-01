package kernel_test

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity/signals"
)

func TestIngestValidationSignal_Activity(t *testing.T) {
	store := signals.NewMemoryStore()
	acts := &kernel.Activities{
		SignalStore:     store,
		SignalAllowlist: signals.DefaultAllowlist(),
	}
	out, err := acts.IngestValidationSignal(t.Context(), kernel.IngestValidationSignalInput{
		WorkflowID:     "wf-1",
		IdempotencyKey: "k1",
		Request: signals.IngestRequest{
			OpportunityID:  "opp-1",
			Class:          signals.ClassPricingCTA,
			SourceIdentity: "stripe-export",
			SourceRef:      "evt_1",
			ExperimentID:   "exp-price",
			Hypothesis:     "CTA converts",
			SampleSize:     3,
			SampleDenom:    50,
			ObservedAt:     time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			Currency:       "USD",
			Environment:    signals.EnvReal,
			RawArtifact:    []byte(`{"clicks":3}`),
			IdempotencyKey: "k1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Eligible || out.SignalID == "" {
		t.Fatalf("out = %+v", out)
	}
	v := kernel.StoreRealSignalVerifier{Store: store, Allowlist: signals.DefaultAllowlist()}
	ok, err := v.HasAllowlistedRealSignal(t.Context(), "opp-1")
	if err != nil || !ok {
		t.Fatalf("verifier: %v %v", ok, err)
	}
}

func TestAcquireValidationSignal_RefusesUnsolicited(t *testing.T) {
	acts := &kernel.Activities{
		SignalStore: signals.NewMemoryStore(),
	}
	out, err := acts.AcquireValidationSignal(t.Context(), kernel.AcquireValidationSignalInput{
		WorkflowID: "wf-1",
		Request: signals.AcquisitionRequest{
			OpportunityID: "opp-1",
			Class:         signals.ClassTrafficExperiment,
			Caps:          signals.ExperimentCaps{MaxSpendUSD: 10},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Refused {
		t.Fatal("expected refusal before external call")
	}
}
