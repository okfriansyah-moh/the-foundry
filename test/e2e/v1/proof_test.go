package v1_test

import (
	"os"
	"testing"
)

// TestProofHarness_RequiresProtectedEnv documents that unit invocation without
// credentials must not fabricate PASS (docs/PLAN.md Task 151).
func TestProofHarness_RequiresProtectedEnv(t *testing.T) {
	if os.Getenv("V1_PROOF_LIVE") != "1" {
		t.Skip("set V1_PROOF_LIVE=1 with real credentials to execute Proofs A–F")
	}
	t.Fatal("live proof body not executed in this environment — configure disposable credentials and re-run make v1-proof")
}

func TestFaultMatrix_RequiresProtectedEnv(t *testing.T) {
	if os.Getenv("V1_PROOF_LIVE") != "1" {
		t.Skip("fault matrix requires V1_PROOF_LIVE=1")
	}
}
