package mission

import (
	"testing"
	"time"
)

func sampleChecklist() CeremonyChecklist {
	return CeremonyChecklist{
		Groups: []CeremonyGroup{
			{
				Name: "authority",
				Items: []CeremonyItem{
					{Key: "production-deploy-permission", Required: true},
					{Key: "billing-permission", Required: true},
				},
			},
		},
	}
}

func TestBuildMissionReadinessArtifact_CeremonyPass(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	artifact, err := BuildMissionReadinessArtifact("m-1", "principal:alice", sampleChecklist(), map[string]CeremonyAnswer{
		"production-deploy-permission": {Resolved: true, Evidence: "doc://policy/deploy", Principal: "principal:alice"},
		"billing-permission":           {Resolved: true, Evidence: "doc://policy/billing", Principal: "principal:alice"},
	}, nil, now)
	if err != nil {
		t.Fatalf("BuildMissionReadinessArtifact: %v", err)
	}
	if !artifact.IsPassing() {
		t.Fatalf("Readiness = %q, want %q", artifact.Readiness, ReadinessPass)
	}
	if artifact.Digest == "" {
		t.Fatal("Digest is empty")
	}
}

func TestBuildMissionReadinessArtifact_CeremonyDeferredRequiredFails(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	artifact, err := BuildMissionReadinessArtifact("m-2", "principal:bob", sampleChecklist(), map[string]CeremonyAnswer{
		"production-deploy-permission": {Deferred: true, Reason: "awaiting legal signoff", RevisitWhen: "2026-08-01"},
		"billing-permission":           {Resolved: true, Evidence: "doc://policy/billing", Principal: "principal:bob"},
	}, nil, now)
	if err != nil {
		t.Fatalf("BuildMissionReadinessArtifact: %v", err)
	}
	if artifact.Readiness != ReadinessFail {
		t.Fatalf("Readiness = %q, want %q", artifact.Readiness, ReadinessFail)
	}
	if len(artifact.DeferredGates) != 1 {
		t.Fatalf("Deferred gate count = %d, want 1", len(artifact.DeferredGates))
	}
}

func TestBuildMissionReadinessArtifact_CeremonyCarriesForwardUnresolvedUnforeseenGate(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	artifact, err := BuildMissionReadinessArtifact("m-3", "principal:carol", sampleChecklist(), map[string]CeremonyAnswer{
		"production-deploy-permission": {Resolved: true, Evidence: "doc://policy/deploy", Principal: "principal:carol"},
		"billing-permission":           {Resolved: true, Evidence: "doc://policy/billing", Principal: "principal:carol"},
	}, []GateEvent{
		{Action: "register-business-bank-account", OccurredAt: now},
	}, now)
	if err != nil {
		t.Fatalf("BuildMissionReadinessArtifact: %v", err)
	}
	if artifact.Readiness != ReadinessFail {
		t.Fatalf("Readiness = %q, want %q", artifact.Readiness, ReadinessFail)
	}
	if len(artifact.DeferredGates) != 1 {
		t.Fatalf("Deferred gate count = %d, want 1", len(artifact.DeferredGates))
	}
	if got := artifact.DeferredGates[0].Gate; got != "unforeseen-human-gate:register-business-bank-account" {
		t.Fatalf("Deferred gate = %q, want unforeseen marker", got)
	}
}
