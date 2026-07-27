package provenance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// TestAppendApprover_RecordsAndResigns is docs/PLAN.md Task 25 Step 3:
// appending an approval record must land in ApprovedPlan.Approvers and
// the artifact must still verify afterward (re-signed, not just mutated).
func TestAppendApprover_RecordsAndResigns(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	before := len(ap.Approvers())
	approver := provenance.Approver{
		Principal:     "bob",
		Method:        "oidc+webauthn",
		At:            time.Now().UTC(),
		AssertionHash: "deadbeef",
	}
	if err := provenance.AppendApprover(kp.Private, ap, approver); err != nil {
		t.Fatalf("AppendApprover: %v", err)
	}

	approvers := ap.Approvers()
	if len(approvers) != before+1 {
		t.Fatalf("approvers count = %d, want %d", len(approvers), before+1)
	}
	last := approvers[len(approvers)-1]
	if last.Principal != "bob" || last.Method != "oidc+webauthn" || last.AssertionHash != "deadbeef" {
		t.Fatalf("appended approver = %+v, want the one just added", last)
	}

	if err := provenance.Verify(kp.Public, ap); err != nil {
		t.Fatalf("expected re-signed plan to verify, got: %v", err)
	}
}

// TestAppendApprover_RejectsEmptyPrincipalOrMethod guards against a
// silently-malformed approval record entering the signed artifact.
func TestAppendApprover_RejectsEmptyPrincipalOrMethod(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	if err := provenance.AppendApprover(kp.Private, ap, provenance.Approver{Method: "oidc"}); err == nil {
		t.Fatal("expected empty principal to be rejected")
	}
	if err := provenance.AppendApprover(kp.Private, ap, provenance.Approver{Principal: "bob"}); err == nil {
		t.Fatal("expected empty method to be rejected")
	}
}

// TestStore_AddApprover_PersistsAndSurvivesLoad is the Store-level
// round-trip: AddApprover must persist through the RawStore and a
// subsequent Load must see it (and still pass signature verification).
func TestStore_AddApprover_PersistsAndSurvivesLoad(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	approver := provenance.Approver{Principal: "carol", Method: "oidc+webauthn", At: time.Now().UTC(), AssertionHash: "abc123"}
	updated, err := store.AddApprover(ctx, ap.PlanID(), kp.Private, approver)
	if err != nil {
		t.Fatalf("AddApprover: %v", err)
	}
	found := false
	for _, a := range updated.Approvers() {
		if a.Principal == "carol" && a.AssertionHash == "abc123" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AddApprover's return value missing the new approver: %+v", updated.Approvers())
	}

	reloaded, err := store.Load(ctx, ap.PlanID())
	if err != nil {
		t.Fatalf("Load after AddApprover: %v", err)
	}
	found = false
	for _, a := range reloaded.Approvers() {
		if a.Principal == "carol" && a.AssertionHash == "abc123" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reloaded plan missing the appended approver: %+v", reloaded.Approvers())
	}
}

// TestStore_AddApprover_RejectsRevokedPlan closes the secondary-review
// finding on Task 25 (docs/PLAN.md Task 25 Status line, "Secondary
// AI-agent review"): AddApprover must not append a new approver or
// re-sign a plan that has already been revoked, mirroring Store.Load's
// own ErrPlanRevoked gate exactly.
func TestStore_AddApprover_RejectsRevokedPlan(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := store.Revoke(ctx, ap.PlanID(), kp.Private, "security-team", "compromised credential"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	before, err := raw.Load(ctx, ap.PlanID())
	if err != nil {
		t.Fatalf("raw load before AddApprover attempt: %v", err)
	}
	beforeCount := len(before.Approvers())

	approver := provenance.Approver{Principal: "mallory", Method: "oidc+webauthn", At: time.Now().UTC(), AssertionHash: "should-not-land"}
	if _, err := store.AddApprover(ctx, ap.PlanID(), kp.Private, approver); err == nil {
		t.Fatal("expected AddApprover to reject an already-revoked plan")
	} else if !errors.Is(err, provenance.ErrPlanRevoked) {
		t.Fatalf("expected errors.Is(err, ErrPlanRevoked), got %v", err)
	}

	after, err := raw.Load(ctx, ap.PlanID())
	if err != nil {
		t.Fatalf("raw load after rejected AddApprover: %v", err)
	}
	for _, a := range after.Approvers() {
		if a.Principal == "mallory" {
			t.Fatalf("approver was appended despite revoked plan: %+v", after.Approvers())
		}
	}
	if len(after.Approvers()) != beforeCount {
		t.Fatalf("approvers count changed: before=%d after=%d", beforeCount, len(after.Approvers()))
	}
}

// TestStore_AddApprover_RejectsExpiredPlan mirrors the revoked-plan case
// for expiry: an already-expired plan must reject AddApprover the same
// way Store.Load already rejects it (docs/PLAN.md Task 25 Status line,
// "Secondary AI-agent review").
func TestStore_AddApprover_RejectsExpiredPlan(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedExpiringAt(t, doc, mustLoadAllowList(t), time.Now().Add(-time.Hour))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	beforeCount := len(ap.Approvers())

	approver := provenance.Approver{Principal: "mallory", Method: "oidc+webauthn", At: time.Now().UTC(), AssertionHash: "should-not-land"}
	if _, err := store.AddApprover(ctx, ap.PlanID(), kp.Private, approver); err == nil {
		t.Fatal("expected AddApprover to reject an already-expired plan")
	} else if !errors.Is(err, provenance.ErrPlanExpired) {
		t.Fatalf("expected errors.Is(err, ErrPlanExpired), got %v", err)
	}

	after, err := raw.Load(ctx, ap.PlanID())
	if err != nil {
		t.Fatalf("raw load after rejected AddApprover: %v", err)
	}
	for _, a := range after.Approvers() {
		if a.Principal == "mallory" {
			t.Fatalf("approver was appended despite expired plan: %+v", after.Approvers())
		}
	}
	if len(after.Approvers()) != beforeCount {
		t.Fatalf("approvers count changed: before=%d after=%d", beforeCount, len(after.Approvers()))
	}
}
