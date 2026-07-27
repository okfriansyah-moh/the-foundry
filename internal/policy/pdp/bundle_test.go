package pdp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/policy"
)

func writeRegoFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

const minimalRego = `package foundry.authz

default allow = false
default reason = "denied"

allow {
	input.action == "permission"
	input.resource == input.policy.permissions_allowlist[_]
}

reason = "allowed" {
	allow
}
`

func TestBundleDigest_DeterministicRegardlessOfFileOrder(t *testing.T) {
	dir := t.TempDir()
	writeRegoFixture(t, dir, "a.rego", minimalRego)
	writeRegoFixture(t, dir, "z.rego", "package foundry.other\n")

	d1, err := BundleDigest(dir)
	if err != nil {
		t.Fatalf("BundleDigest: %v", err)
	}
	d2, err := BundleDigest(dir)
	if err != nil {
		t.Fatalf("BundleDigest: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("BundleDigest not deterministic: %s != %s", d1, d2)
	}
}

func TestNewOPADecider_RefusesOnDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	writeRegoFixture(t, dir, "a.rego", minimalRego)

	resolved := testResolved(t)
	_, err := NewOPADecider(context.Background(), dir, "sha256:wrong-digest", resolved)
	if err == nil {
		t.Fatal("expected NewOPADecider to refuse construction on a pinned-digest mismatch")
	}
}

// TestVerifyIntegrity_DetectsPostBootTamper proves this package's tamper-
// evidence requirement: a rego file modified after a Decider has already
// booted is detectable via VerifyIntegrity, and Decide itself never
// silently starts using the modified content (it evaluates only the
// compiled query captured at boot).
func TestVerifyIntegrity_DetectsPostBootTamper(t *testing.T) {
	dir := t.TempDir()
	writeRegoFixture(t, dir, "a.rego", minimalRego)

	resolved := testResolved(t)
	digest, err := BundleDigest(dir)
	if err != nil {
		t.Fatalf("BundleDigest: %v", err)
	}
	d, err := NewOPADecider(context.Background(), dir, digest, resolved)
	if err != nil {
		t.Fatalf("NewOPADecider: %v", err)
	}

	if err := d.VerifyIntegrity(); err != nil {
		t.Fatalf("VerifyIntegrity on untouched bundle: %v", err)
	}

	req := policy.Request{Action: "permission", Resource: "repo-read", PolicyDigest: resolved.Digest}
	before, err := d.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide before tamper: %v", err)
	}

	// Tamper: widen the allowlist check by rewriting the file after boot.
	writeRegoFixture(t, dir, "a.rego", `package foundry.authz

default allow = true
default reason = "tampered: always allowed"
`)

	if err := d.VerifyIntegrity(); err == nil {
		t.Fatal("expected VerifyIntegrity to detect the post-boot rego file change")
	}

	// Decide must still reflect the boot-time compiled query, not the
	// tampered file on disk — proving the tamper was never silently used.
	after, err := d.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide after tamper: %v", err)
	}
	if before != after {
		t.Fatalf("Decide changed after an on-disk tamper without a rebuild: before=%+v after=%+v", before, after)
	}
}
