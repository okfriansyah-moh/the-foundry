package productpackaging_test

import (
	"os"
	"os/exec"
	"testing"
)

func TestProductPackagingE2E(t *testing.T) {
	cmd := exec.Command("bash", "run.sh")
	cmd.Env = append(os.Environ(), "FOUNDRY_PACKAGING_EVIDENCE=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("product packaging e2e: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
