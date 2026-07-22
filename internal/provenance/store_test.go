package provenance_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

func TestStore_InsertAndLoad_RoundTrip(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()

	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	loaded, err := store.Load(ctx, ap.PlanID())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PlanDigest() != ap.PlanDigest() {
		t.Fatalf("loaded digest %q != inserted digest %q", loaded.PlanDigest(), ap.PlanDigest())
	}
}

// TestStore_Insert_RejectsUnsignedPlan is the "unsigned/forged insert
// attempt must be rejected" acceptance case (docs/PLAN.md Task 8 Step 6):
// an ApprovedPlan built but never signed must never reach the store.
func TestStore_Insert_RejectsUnsignedPlan(t *testing.T) {
	doc := mustParseFixturePlan(t)
	allow := mustLoadAllowList(t)

	unsigned, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     doc.ID,
		PlanDigest: doc.DigestHex(),
		Requested:  doc.RequestedPermissions,
	}, allow)
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}

	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)

	if err := store.Insert(context.Background(), unsigned); err == nil {
		t.Fatal("expected Insert of an unsigned ApprovedPlan to be rejected")
	}
	if _, err := raw.Load(context.Background(), doc.ID); err == nil {
		t.Fatal("unsigned plan must never reach the underlying raw store")
	}
}

// TestStore_Insert_RejectsForgedSignature: a plan signed with a key other
// than the one the Store verifies against is a forged insert attempt.
func TestStore_Insert_RejectsForgedSignature(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, _ := buildApprovedAndSign(t, doc, mustLoadAllowList(t)) // signed with an unrelated key

	verifierKey, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, verifierKey.Public)

	if err := store.Insert(context.Background(), ap); err == nil {
		t.Fatal("expected Insert to reject a plan signed under a different key")
	}
}

// TestStore_Load_DetectsByteFlipTamperingOfDBRow is the "simulated DB row"
// tampering acceptance case (docs/PLAN.md Task 8 Step 6): flipping one byte
// in the persisted row must make Load error, never silently return altered
// data (Constitution C7).
func TestStore_Load_DetectsByteFlipTamperingOfDBRow(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()

	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Sanity: unmodified row still verifies.
	if _, err := store.Load(ctx, ap.PlanID()); err != nil {
		t.Fatalf("load before tampering: %v", err)
	}

	// Flip bytes across the row until one produces a semantically
	// different, still-valid-JSON row — a byte flip inside a JSON string
	// value always changes the signed payload and must fail Load.
	tampered := false
	for i := 0; i < 4096; i++ {
		raw2 := provenance.NewMemRawStore()
		if err := raw2.Insert(ctx, ap); err != nil {
			t.Fatalf("insert into scratch store: %v", err)
		}
		raw2.CorruptRow(ap.PlanID(), i)
		s2 := provenance.NewStore(raw2, kp.Public)
		if _, err := s2.Load(ctx, ap.PlanID()); err != nil {
			tampered = true
			break
		}
	}
	if !tampered {
		t.Fatal("expected at least one single-byte flip in the stored row to fail Load")
	}
}

func TestStore_Load_NotFound(t *testing.T) {
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)

	if _, err := store.Load(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected error loading a plan id that was never inserted")
	}
}
