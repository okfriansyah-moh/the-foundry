package observe_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

func TestHumanTouchCounter_AvoidableZero(t *testing.T) {
	var c observe.HumanTouchCounter
	c.Record(observe.TouchUnavoidable, "readiness_ceremony", false)
	if c.AvoidableCount() != 0 {
		t.Fatalf("avoidable = %d", c.AvoidableCount())
	}
	dir := t.TempDir()
	if err := c.WriteEvidence(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "human-touches.json")); err != nil {
		t.Fatal(err)
	}
}

func TestHumanTouchCounter_CountsAvoidable(t *testing.T) {
	var c observe.HumanTouchCounter
	c.Record(observe.TouchBlockingGate, "budget_raise", true)
	c.Record(observe.TouchUnavoidable, "h_tier_approval", false)
	if c.AvoidableCount() != 1 {
		t.Fatalf("avoidable = %d", c.AvoidableCount())
	}
}
