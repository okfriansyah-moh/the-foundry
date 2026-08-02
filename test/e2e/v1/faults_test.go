package v1_test

import (
	"os"
	"testing"
)

func TestFaults_PersonalAndTenX(t *testing.T) {
	if os.Getenv("V1_PROOF_LIVE") != "1" {
		t.Skip("protected fault matrix gated")
	}
}
