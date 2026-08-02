package kernel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

func envelopeTestPolicy(t *testing.T) *compiler.Resolved {
	t.Helper()
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("platform defaults: %v", err)
	}
	requireSandbox := true
	profile := compiler.LayerPolicy{RequireSandbox: &requireSandbox}
	resolved, err := compiler.Compile(platform, compiler.LayerPolicy{}, profile, compiler.LayerPolicy{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return resolved
}

func envelopeApprovedStore(t *testing.T, planID, digest string, profileKind string) (*provenance.Store, *provenance.KeyPair) {
	t.Helper()
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	now := time.Now().UTC()
	approved, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:              planID,
		PlanDigest:          digest,
		CreatorPrincipal:    "alice",
		SubmittingPrincipal: "alice",
		ClassifierVersion:   "v1",
		Scope:               provenance.Scope{Repositories: []string{"https://github.com/example/x"}},
		RiskTier:           admission.TierA0,
		ProfileKind:         profileKind,
		BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: 10, WorkflowUSD: 5},
		DataClass:           "internal",
		Approvers:           []provenance.Approver{{Principal: "alice", Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:          now,
		ExpiresAt:           now.Add(24 * time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("new plan: %v", err)
	}
	if err := provenance.Sign(kp.Private, approved); err != nil {
		t.Fatalf("sign: %v", err)
	}
	store := provenance.NewStore(provenance.NewMemRawStore(), kp.Public)
	if err := store.Insert(context.Background(), approved); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return store, kp
}

func TestExecutionEnvelope_CanonicalDigestStable(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-env-1", "digest-env-1", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	in := kernel.ResolveExecutionEnvelopeInput{
		PlanID:             "plan-env-1",
		PlanArtifactRef:    "artifact:plan-env-1",
		RepositoryID:       "repo-1",
		Provider:           kernel.ProviderGitHub,
		CanonicalURL:       "https://github.com/example/x",
		PinnedBaseRevision: "abc123",
		MissionID:          "mission-1",
		ProfileID:          "personal-autonomous-venture",
		PrincipalID:        "alice",
		Unattended:         true,
		IssuedAt:           fixed,
	}
	a, err := kernel.ResolveExecutionEnvelope(context.Background(), kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, Now: func() time.Time { return fixed },
	}, in)
	if err != nil {
		t.Fatalf("resolve a: %v", err)
	}
	b, err := kernel.ResolveExecutionEnvelope(context.Background(), kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, Now: func() time.Time { return fixed },
	}, in)
	if err != nil {
		t.Fatalf("resolve b: %v", err)
	}
	if a.EnvelopeDigest == "" || a.EnvelopeDigest != b.EnvelopeDigest {
		t.Fatalf("digest unstable: %q vs %q", a.EnvelopeDigest, b.EnvelopeDigest)
	}
	if a.SchemaVersion != kernel.ExecutionEnvelopeSchemaV1 {
		t.Fatalf("schema = %q", a.SchemaVersion)
	}
	if !a.Execution.RequireSandbox {
		t.Fatal("unattended autonomous profile must require sandbox")
	}
}

func TestExecutionEnvelope_RefuseMissingSources(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-env-2", "digest-env-2", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	base := kernel.ResolveExecutionEnvelopeInput{
		PlanID: "plan-env-2", PlanArtifactRef: "artifact:x",
		RepositoryID: "r1", Provider: kernel.ProviderGitHub, CanonicalURL: "https://github.com/example/x",
		PinnedBaseRevision: "abc", MissionID: "m1", ProfileID: "personal-autonomous-venture",
		Unattended: true, IssuedAt: fixed,
	}

	cases := []struct {
		name string
		deps kernel.EnvelopeResolverDeps
		in   kernel.ResolveExecutionEnvelopeInput
	}{
		{"missing policy", kernel.EnvelopeResolverDeps{Provenance: store}, base},
		{"missing plan", kernel.EnvelopeResolverDeps{Provenance: store, Policy: policy}, kernel.ResolveExecutionEnvelopeInput{Unattended: true, IssuedAt: fixed}},
		{"missing revision", kernel.EnvelopeResolverDeps{Provenance: store, Policy: policy}, func() kernel.ResolveExecutionEnvelopeInput {
			in := base
			in.PinnedBaseRevision = ""
			return in
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := kernel.ResolveExecutionEnvelope(context.Background(), tc.deps, tc.in)
			if !errors.Is(err, kernel.ErrEnvelopeRefused) {
				t.Fatalf("want ErrEnvelopeRefused, got %v", err)
			}
		})
	}
}

func TestExecutionEnvelope_SandboxRequiredCannotBeFalse(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-env-3", "digest-env-3", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	falseVal := false
	_, err := kernel.ResolveExecutionEnvelope(context.Background(), kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, Now: func() time.Time { return fixed },
	}, kernel.ResolveExecutionEnvelopeInput{
		PlanID: "plan-env-3", PlanArtifactRef: "a", RepositoryID: "r", Provider: kernel.ProviderGitHub,
		CanonicalURL: "https://github.com/example/x", PinnedBaseRevision: "abc",
		MissionID: "m", ProfileID: "personal-autonomous-venture", Unattended: true,
		RequireSandbox: &falseVal, IssuedAt: fixed,
	})
	if !errors.Is(err, kernel.ErrEnvelopeRefused) {
		t.Fatalf("want refuse sandbox=false, got %v", err)
	}
}

func TestExecutionEnvelope_OwnershipMismatch(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-env-4", "digest-env-4", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	_, err := kernel.ResolveExecutionEnvelope(context.Background(), kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, Now: func() time.Time { return fixed },
	}, kernel.ResolveExecutionEnvelopeInput{
		PlanID: "plan-env-4", PlanArtifactRef: "a", RepositoryID: "r", Provider: kernel.ProviderGitHub,
		CanonicalURL: "https://github.com/example/x", PinnedBaseRevision: "abc",
		MissionID: "m", ProfileID: "personal-autonomous-venture", OrganizationID: "org-1",
		Unattended: true, IssuedAt: fixed,
	})
	if !errors.Is(err, kernel.ErrEnvelopeRefused) {
		t.Fatalf("want ownership mismatch refuse, got %v", err)
	}
}

func TestExecutionEnvelopeStore_ImmutableAndTamper(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-env-5", "digest-env-5", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	env, err := kernel.ResolveExecutionEnvelope(context.Background(), kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, Now: func() time.Time { return fixed },
	}, kernel.ResolveExecutionEnvelopeInput{
		PlanID: "plan-env-5", PlanArtifactRef: "a", RepositoryID: "r", Provider: kernel.ProviderLocal,
		CanonicalURL: "file:///tmp/x", PinnedBaseRevision: "abc", MissionID: "m",
		ProfileID: "personal-autonomous-venture", Unattended: true, IssuedAt: fixed,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	mem := kernel.NewMemEnvelopeStore()
	if err := mem.Insert(context.Background(), env); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := mem.Insert(context.Background(), env); err == nil {
		t.Fatal("duplicate insert must fail")
	}
	loaded, revoked, err := mem.LoadByDigest(context.Background(), env.EnvelopeDigest)
	if err != nil || revoked {
		t.Fatalf("load: %v revoked=%v", err, revoked)
	}
	loaded.Execution.RequireSandbox = false
	if err := loaded.Validate(); !errors.Is(err, kernel.ErrEnvelopeTampered) {
		t.Fatalf("want tamper, got %v", err)
	}
	if err := mem.Revoke(context.Background(), env.EnvelopeID, "test", fixed); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err = kernel.LoadAndVerifyEnvelope(context.Background(), mem, env.EnvelopeDigest, fixed)
	if !errors.Is(err, kernel.ErrEnvelopeRefused) {
		t.Fatalf("want refuse revoked, got %v", err)
	}
}

func TestExecutionEnvelope_TransportWideningRefused(t *testing.T) {
	store, _ := envelopeApprovedStore(t, "plan-env-6", "digest-env-6", "personal")
	policy := envelopeTestPolicy(t)
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	env, err := kernel.ResolveExecutionEnvelope(context.Background(), kernel.EnvelopeResolverDeps{
		Provenance: store, Policy: policy, Now: func() time.Time { return fixed },
	}, kernel.ResolveExecutionEnvelopeInput{
		PlanID: "plan-env-6", PlanArtifactRef: "a", RepositoryID: "r", Provider: kernel.ProviderLocal,
		CanonicalURL: "file:///tmp/x", PinnedBaseRevision: "abc", MissionID: "m",
		ProfileID: "personal-autonomous-venture", Unattended: false, SessionCapUSD: 5,
		MaxWaveConcurrency: 2, IssuedAt: fixed,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	falseVal := false
	if err := env.RefuseTransportWidening(kernel.ResolveExecutionEnvelopeInput{
		Unattended: true, RequireSandbox: &falseVal, MaxWaveConcurrency: 99, SessionCapUSD: 50,
	}); !errors.Is(err, kernel.ErrEnvelopeRefused) {
		t.Fatalf("want widening refuse, got %v", err)
	}
}

func TestStartDelivery_UnattendedWithoutEnvelopeRefused(t *testing.T) {
	store, _, _ := newApprovedPlanStore(t)
	deps := baseDeps(t, &fakeStarter{}, store)
	_, err := kernel.StartDelivery(context.Background(), deps, kernel.StartDeliveryInput{
		PlanID: "plan-start-1", Unattended: true, MissionID: "m1",
	})
	if !errors.Is(err, kernel.ErrStartRefused) {
		t.Fatalf("want ErrStartRefused, got %v", err)
	}
}

func TestStartDelivery_ResolvesAndBindsEnvelope(t *testing.T) {
	store, _, digest := newApprovedPlanStore(t)
	policy := envelopeTestPolicy(t)
	starter := &fakeStarter{}
	deps := baseDeps(t, starter, store)
	deps.Policy = policy
	deps.EnvelopeStore = kernel.NewMemEnvelopeStore()
	deps.Now = func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }

	out, err := kernel.StartDelivery(context.Background(), deps, kernel.StartDeliveryInput{
		PlanID: "plan-start-1", MissionID: "mission-charge", ProfileID: "personal-autonomous-venture",
		Unattended: true, RepositoryID: "repo-1", Provider: kernel.ProviderLocal,
		CanonicalURL: "file:///tmp/x", PinnedBaseRevision: "deadbeef",
	})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	if out.EnvelopeDigest == "" || out.EnvelopeID == "" {
		t.Fatalf("missing envelope on output: %+v", out)
	}
	wantID := kernel.DeliveryWorkflowID(digest, 0, out.EnvelopeDigest)
	if out.WorkflowID != wantID {
		t.Fatalf("workflow id = %q, want envelope-bound %q", out.WorkflowID, wantID)
	}
	if len(starter.lastArgs) != 1 {
		t.Fatalf("expected DeliverPlanInput arg, got %#v", starter.lastArgs)
	}
	in, ok := starter.lastArgs[0].(kernel.DeliverPlanInput)
	if !ok {
		t.Fatalf("arg type %T", starter.lastArgs[0])
	}
	if in.EnvelopeDigest != out.EnvelopeDigest || !in.RequireSandbox || in.MissionID != "mission-charge" {
		t.Fatalf("DeliverPlanInput not envelope-derived: %+v", in)
	}
	if in.BudgetScopeID != "mission-charge" {
		t.Fatalf("budget scope id = %q, want mission-charge", in.BudgetScopeID)
	}
}
