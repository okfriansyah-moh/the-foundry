package provenance_test

import (
	"os"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// fixturePlan is a minimal valid plan.Document usable across tests. It
// requests two permissions: one covered by the test allowlist, one not —
// exercising the Requested ∩ allow computation.
const fixturePlanSource = `---
id: plan-fixture-provenance
title: Fixture provenance plan
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/fixture
    branch: main
tasks:
  - id: t1
    goal: Fixture task
    commands:
      - echo noop
    validation_commands:
      - echo ok
    files:
      - internal/example/file.go
declared_effects:
  - kind: docs
    target: README.md
requested_permissions:
  - kind: repo-read
    target: "*"
  - kind: billing-write
    target: "*"
budget_usd: 5.0
---
## Rationale

Fixture for internal/provenance tests.
`

// testAllowList allows repo-read but not billing-write, so Requested has
// two entries and Granted must have exactly one.
const testAllowListSource = `
permissions:
  - kind: repo-read
    target: "*"
`

func mustParseFixturePlan(t *testing.T) *plan.Document {
	t.Helper()
	doc, err := plan.ParseBytes([]byte(fixturePlanSource))
	if err != nil {
		t.Fatalf("parse fixture plan: %v", err)
	}
	return doc
}

func mustLoadAllowList(t *testing.T) provenance.AllowList {
	t.Helper()
	path := t.TempDir() + "/permissions-allowlist.yaml"
	if err := os.WriteFile(path, []byte(testAllowListSource), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	al, err := provenance.LoadAllowList(path)
	if err != nil {
		t.Fatalf("load allowlist: %v", err)
	}
	return al
}

// buildApprovedAndSign builds, signs, and returns an ApprovedPlan for doc
// plus the key pair used to sign it.
func buildApprovedAndSign(t *testing.T, doc *plan.Document, allow provenance.AllowList) (*provenance.ApprovedPlan, *provenance.KeyPair) {
	t.Helper()
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	decision, err := admission.Classify(doc, admission.NoopPolicyView{})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}

	now := time.Now().UTC()
	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:              doc.ID,
		PlanDigest:          doc.DigestHex(),
		CreatorPrincipal:    "alice",
		SubmittingPrincipal: "alice",
		ClassifierVersion:   decision.ClassifierVersion,
		Declared:            decision.Declared,
		Requested:           doc.RequestedPermissions,
		Scope:               provenance.Scope{Repositories: []string{"https://github.com/example/fixture"}},
		RiskTier:            decision.Tier,
		BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: doc.BudgetUSD, WorkflowUSD: doc.BudgetUSD},
		DataClass:           "internal",
		Approvers:           []provenance.Approver{{Principal: "alice", Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:          now,
		ExpiresAt:           now.Add(24 * time.Hour),
	}, allow)
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}
	if err := provenance.Sign(kp.Private, ap); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return ap, kp
}
