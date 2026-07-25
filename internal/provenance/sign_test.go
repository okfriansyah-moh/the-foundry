package provenance_test

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

func TestSignAndVerify_RoundTrip(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	if err := provenance.Verify(kp.Public, ap); err != nil {
		t.Fatalf("verify freshly signed plan: %v", err)
	}
}

func TestVerify_WrongKeyFails(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, _ := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	other, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate other key pair: %v", err)
	}
	if err := provenance.Verify(other.Public, ap); err == nil {
		t.Fatal("expected verification under a different public key to fail")
	}
}

func TestVerify_MissingSignatureFails(t *testing.T) {
	doc := mustParseFixturePlan(t)
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	ap, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     doc.ID,
		PlanDigest: doc.DigestHex(),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}
	if err := provenance.Verify(kp.Public, ap); err == nil {
		t.Fatal("expected verify of an unsigned ApprovedPlan to fail")
	}
}

func TestSigningPayload_ExcludesSignatureAndIsDeterministic(t *testing.T) {
	doc := mustParseFixturePlan(t)
	allow := mustLoadAllowList(t)
	ap1, _ := buildApprovedAndSign(t, doc, allow)

	p1, err := provenance.SigningPayload(ap1)
	if err != nil {
		t.Fatalf("signing payload: %v", err)
	}
	p2, err := provenance.SigningPayload(ap1)
	if err != nil {
		t.Fatalf("signing payload: %v", err)
	}
	if string(p1) != string(p2) {
		t.Fatal("SigningPayload is not deterministic for the same ApprovedPlan")
	}
}

func TestKeyPair_GenerateWriteLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	if err := provenance.WriteKeyPair(dir, kp, false); err != nil {
		t.Fatalf("write key pair: %v", err)
	}

	// 0600 perms on both files (docs/PLAN.md Task 8 Step 2).
	for _, name := range []string{"approver.pub", "approver.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s: expected perm 0600, got %o", name, perm)
		}
	}

	loaded, err := provenance.LoadKeyPair(dir)
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}
	if !ed25519.PublicKey(loaded.Public).Equal(kp.Public) {
		t.Fatal("loaded public key does not match generated public key")
	}

	// Refuses to overwrite without --force.
	if err := provenance.WriteKeyPair(dir, kp, false); err == nil {
		t.Fatal("expected WriteKeyPair to refuse overwriting an existing key pair")
	}
	if err := provenance.WriteKeyPair(dir, kp, true); err != nil {
		t.Fatalf("expected WriteKeyPair with force=true to succeed: %v", err)
	}
}
