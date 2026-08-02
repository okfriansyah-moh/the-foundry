package profile

import "testing"

func TestRuntimeIsolation_ValidateStartup(t *testing.T) {
	r := RuntimeIsolation{
		ProfileID: "p1", Kind: Personal,
		DBIdentity: "db-p1", TemporalNamespace: "ns-p1",
		EvidencePrefix: "evidence/p1/", SecretScope: "sec-p1",
		LedgerScope: "led-p1", PortfolioScope: "port-p1", AuditChainID: "audit-p1",
	}
	if err := r.ValidateStartup(); err != nil {
		t.Fatal(err)
	}
	r.EvidencePrefix = "*"
	if err := r.ValidateStartup(); err == nil {
		t.Fatal("expected global evidence prefix refusal")
	}
}

func TestRuntimeIsolation_EnvelopeCompatibility(t *testing.T) {
	r := RuntimeIsolation{ProfileID: "p1", Kind: Personal}
	if err := r.CompatibleWithEnvelope("p2", ""); err == nil {
		t.Fatal("expected wrong profile refusal")
	}
	if err := r.CompatibleWithEnvelope("p1", "org"); err == nil {
		t.Fatal("expected org on personal refusal")
	}
}

func TestRefuseMultiProfileSingleProcess(t *testing.T) {
	a := RuntimeIsolation{
		ProfileID: "a", Kind: Personal, DBIdentity: "db-a", TemporalNamespace: "ns-a",
		EvidencePrefix: "e/a/", SecretScope: "s-a", LedgerScope: "l-a", PortfolioScope: "p-a", AuditChainID: "au-a",
	}
	b := RuntimeIsolation{
		ProfileID: "b", Kind: Organization, DBIdentity: "db-a", TemporalNamespace: "ns-b",
		EvidencePrefix: "e/b/", SecretScope: "s-b", LedgerScope: "l-b", PortfolioScope: "p-b", AuditChainID: "au-b",
	}
	if err := RefuseMultiProfileSingleProcess([]RuntimeIsolation{a, b}); err == nil {
		t.Fatal("expected shared db refusal")
	}
}
