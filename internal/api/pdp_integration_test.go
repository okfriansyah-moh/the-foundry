package api

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/policy"
	"github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/policy/pdp"
)

// TestServer_UsesRealOPADecider proves this package's authorize
// middleware works against the real, pinned, tamper-evident OPA bundle
// (internal/policy/pdp, Task 23) — not just the allowAllDecider fake
// every other test in this package uses to isolate routing/handler
// behavior. It exercises config/policy/rego/authz.rego's actual "api"
// action rule (this task's addition), built the same way
// cmd/foundryd.buildAPIServer constructs it in production: platform
// policy layer only, compiled via compiler.Compile, digest-pinned via
// pdp.BundleDigest.
func TestServer_UsesRealOPADecider(t *testing.T) {
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatalf("PlatformDefaults: %v", err)
	}
	resolved, err := compiler.Compile(platform, compiler.LayerPolicy{}, compiler.LayerPolicy{}, compiler.LayerPolicy{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	bundleDir := "../../config/policy/rego"
	digest, err := pdp.BundleDigest(bundleDir)
	if err != nil {
		t.Fatalf("BundleDigest: %v", err)
	}
	decider, err := pdp.NewOPADecider(context.Background(), bundleDir, digest, resolved)
	if err != nil {
		t.Fatalf("NewOPADecider: %v", err)
	}

	got, err := decider.Decide(context.Background(), policy.Request{
		Principal:    "alice@example.com",
		Action:       pdpAction,
		Resource:     "plan:submit",
		PolicyDigest: resolved.Digest,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !got.Allow {
		t.Errorf("real OPA decider denied an authenticated principal on action %q: %+v", pdpAction, got)
	}

	denied, err := decider.Decide(context.Background(), policy.Request{
		Principal:    "",
		Action:       pdpAction,
		Resource:     "plan:submit",
		PolicyDigest: resolved.Digest,
	})
	if err != nil {
		t.Fatalf("Decide (empty principal): %v", err)
	}
	if denied.Allow {
		t.Error("real OPA decider allowed an empty principal, want deny")
	}
}
