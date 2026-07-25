package provenance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// TestVerifyPlanFile_DetectsByteFlipTamperingOfPlanFile is the "tampered
// plan byte ⇒ verify fails" acceptance case (docs/PLAN.md Task 8
// Acceptance): flipping a byte in the on-disk plan file after approval
// must make the recomputed digest stop matching the approved digest.
func TestVerifyPlanFile_DetectsByteFlipTamperingOfPlanFile(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte(fixturePlanSource), 0o600); err != nil {
		t.Fatalf("write plan file: %v", err)
	}

	// Untampered file still verifies and matches.
	result, err := provenance.VerifyPlanFile(ctx, store, ap.PlanID(), path)
	if err != nil {
		t.Fatalf("verify untampered file: %v", err)
	}
	if !result.DigestMatches {
		t.Fatal("expected untampered file digest to match the approved digest")
	}
	if !result.GrantedSubset {
		t.Fatal("expected granted to be a subset of requested")
	}

	// Flip one byte in the fixture body (well away from front matter, so
	// the file still parses) and confirm the digest stops matching.
	tampered := []byte(fixturePlanSource)
	idx := len(tampered) - 5 // inside the trailing rationale text
	tampered[idx] ^= 0xFF
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatalf("write tampered plan file: %v", err)
	}

	result, err = provenance.VerifyPlanFile(ctx, store, ap.PlanID(), path)
	if err != nil {
		t.Fatalf("verify tampered file (parse should still succeed): %v", err)
	}
	if result.DigestMatches {
		t.Fatal("expected tampered file digest to NOT match the approved digest")
	}
}

// TestVerifyPlanFile_TamperedDBRowErrors confirms that the kernel-facing
// Load path used by VerifyPlanFile surfaces DB-row tampering as an error,
// not merely a digest mismatch.
func TestVerifyPlanFile_TamperedDBRowErrors(t *testing.T) {
	doc := mustParseFixturePlan(t)
	ap, kp := buildApprovedAndSign(t, doc, mustLoadAllowList(t))

	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	ctx := context.Background()
	if err := store.Insert(ctx, ap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte(fixturePlanSource), 0o600); err != nil {
		t.Fatalf("write plan file: %v", err)
	}

	raw.CorruptRow(ap.PlanID(), 10)

	if _, err := provenance.VerifyPlanFile(ctx, store, ap.PlanID(), path); err == nil {
		t.Fatal("expected VerifyPlanFile to error when the underlying DB row is tampered")
	}
}
