package validationsignal_test

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity/signals"
)

func TestProductionVerifierUsesStore(t *testing.T) {
	store := signals.NewMemoryStore()
	allow := signals.DefaultAllowlist()
	req := signals.IngestRequest{
		OpportunityID:  "opp-1",
		Class:          signals.ClassWaitlistSignup,
		SourceIdentity: "waitlist-export",
		SourceRef:      "https://example.test/export/1",
		ExperimentID:   "exp-1",
		Hypothesis:     "waitlist converts",
		SampleSize:     12,
		SampleDenom:    100,
		ObservedAt:     time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Environment:    signals.EnvReal,
		RawArtifact:    []byte(`{"signups":12}`),
		IdempotencyKey: "idem-1",
	}
	if _, err := signals.Ingest(context.Background(), store, allow, req, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	v := kernel.StoreRealSignalVerifier{Store: store, Allowlist: allow}
	ok, err := v.HasAllowlistedRealSignal(context.Background(), "opp-1")
	if err != nil || !ok {
		t.Fatalf("want eligible real signal, ok=%v err=%v", ok, err)
	}
}
