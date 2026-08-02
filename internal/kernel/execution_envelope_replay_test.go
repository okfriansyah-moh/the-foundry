package kernel_test

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestExecutionEnvelope_ReplaySameAuthorityDecision proves identical
// authoritative inputs re-resolve to the same digest and authority fields
// (docs/PLAN.md Task 141 Acceptance 20).
func TestExecutionEnvelope_ReplaySameAuthorityDecision(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-replay-1", "digest-replay-1", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 15, 30, 0, 0, time.UTC)
	in := kernel.ResolveExecutionEnvelopeInput{
		PlanID: "plan-replay-1", PlanArtifactRef: "artifact:replay",
		RepositoryID: "repo-replay", Provider: kernel.ProviderBitbucket,
		CanonicalURL: "https://bitbucket.org/example/x", PinnedBaseRevision: "rev1",
		MissionID: "mission-replay", ProfileID: "personal-autonomous-venture",
		PrincipalID: "alice", Unattended: true, MaxWaveConcurrency: 2,
		SessionCapUSD: 10, IssuedAt: fixed,
	}
	deps := kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, LayerDigests: []string{"platform", "profile"},
		Now: func() time.Time { return fixed },
	}

	first, err := kernel.ResolveExecutionEnvelope(context.Background(), deps, in)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := kernel.ResolveExecutionEnvelope(context.Background(), deps, in)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first.EnvelopeDigest != second.EnvelopeDigest {
		t.Fatalf("replay digest drift: %q vs %q", first.EnvelopeDigest, second.EnvelopeDigest)
	}
	if first.Execution.RequireSandbox != second.Execution.RequireSandbox {
		t.Fatal("require_sandbox drifted on replay")
	}
	if len(first.Execution.ExecutorAllowlist) == 0 ||
		len(first.Execution.ExecutorAllowlist) != len(second.Execution.ExecutorAllowlist) {
		t.Fatalf("allowlist drift: %#v vs %#v", first.Execution.ExecutorAllowlist, second.Execution.ExecutorAllowlist)
	}
	for i := range first.Execution.ExecutorAllowlist {
		if first.Execution.ExecutorAllowlist[i] != second.Execution.ExecutorAllowlist[i] {
			t.Fatalf("allowlist order/content drift at %d", i)
		}
	}
	if first.Policy.PolicyDigest != second.Policy.PolicyDigest {
		t.Fatal("policy digest drifted on replay")
	}
	if first.Cost.BudgetScopeID != "mission-replay" {
		t.Fatalf("mission attribution = %q", first.Cost.BudgetScopeID)
	}

	mem := kernel.NewMemEnvelopeStore()
	if err := mem.Insert(context.Background(), first); err != nil {
		t.Fatalf("insert: %v", err)
	}
	loaded, err := kernel.LoadAndVerifyEnvelope(context.Background(), mem, first.EnvelopeDigest, fixed)
	if err != nil {
		t.Fatalf("verify stored: %v", err)
	}
	if loaded.EnvelopeDigest != first.EnvelopeDigest {
		t.Fatalf("stored digest mismatch")
	}
}
