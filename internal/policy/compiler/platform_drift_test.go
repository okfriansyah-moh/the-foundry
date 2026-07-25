package compiler

import (
	"os"
	"testing"
)

// TestEmbeddedPlatformYAMLMatchesCanonicalCopy guards against
// internal/policy/compiler/embedded/platform.yaml (go:embed cannot cross
// "..") drifting from config/policy/platform.yaml, the canonical,
// ops-facing copy docs/PLAN.md Task 22 names. If this fails, copy one
// over the other — never edit only one.
func TestEmbeddedPlatformYAMLMatchesCanonicalCopy(t *testing.T) {
	embedded, err := platformFS.ReadFile("embedded/platform.yaml")
	if err != nil {
		t.Fatalf("read embedded platform.yaml: %v", err)
	}
	canonical, err := os.ReadFile("../../../config/policy/platform.yaml")
	if err != nil {
		t.Fatalf("read canonical platform.yaml: %v", err)
	}
	if string(embedded) != string(canonical) {
		t.Fatalf("internal/policy/compiler/embedded/platform.yaml has drifted from config/policy/platform.yaml")
	}
}
