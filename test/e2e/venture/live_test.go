package venture_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// TestLiveVentureHarnessSkeleton is the gated live entry (Task 132). Without
// RUN_VENTURE_LIVE=1 it skips; with the flag it archives a human-touch ledger
// proving avoidable=0 for the instrument path exercised here.
func TestLiveVentureHarnessSkeleton(t *testing.T) {
	if os.Getenv("RUN_VENTURE_LIVE") != "1" {
		t.Skip("RUN_VENTURE_LIVE=1 not set")
	}
	root := os.Getenv("FOUNDRY_VENTURE_EVIDENCE")
	if root == "" {
		root = filepath.Join("evidence", "m5-personal")
	}
	var c observe.HumanTouchCounter
	c.Record(observe.TouchUnavoidable, "readiness_ceremony", false)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteEvidence(root); err != nil {
		t.Fatal(err)
	}
	if c.AvoidableCount() != 0 {
		t.Fatalf("avoidable touches = %d", c.AvoidableCount())
	}
	raw, err := os.ReadFile(filepath.Join(root, "human-touches.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		AvoidableCount int `json:"avoidable_count"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.AvoidableCount != 0 {
		t.Fatalf("evidence avoidable_count=%d", report.AvoidableCount)
	}
}
