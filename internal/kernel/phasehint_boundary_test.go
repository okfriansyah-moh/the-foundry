package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhaseHintNeverRead is a fitness-style boundary check (docs/PLAN.md Task
// 92 / PRV-09, Step 4): the kernel's own DECISION paths must never read
// TaskPacket.PhaseHint. The hint is one-directional (kernel→executor) and
// carries no authority, so if any of these files referenced PhaseHint it
// would risk the kernel deferring a decision to a non-authoritative label,
// violating C4/C10. The workflow may WRITE PhaseHint onto the outbound packet
// (that is the whole point) — so workflow.go is deliberately excluded; every
// other decision-path file must not mention it at all.
func TestPhaseHintNeverRead(t *testing.T) {
	decisionFiles := []string{
		"executor_select.go", // ExecutorSelector.Select
		"executor_routing.go",
		"activities.go", // ExecuteTask/ValidateTask/RecordEvidence bodies
	}
	for _, name := range decisionFiles {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(data), "PhaseHint") {
			t.Fatalf("%s references PhaseHint — the kernel's decision path must never read the hint (Task 92 boundary)", name)
		}
	}

	// The admission package (risk classification) must likewise never read it.
	admissionDir := filepath.Join("..", "admission")
	entries, err := os.ReadDir(admissionDir)
	if err != nil {
		t.Fatalf("read %s: %v", admissionDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(admissionDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(data), "PhaseHint") {
			t.Fatalf("internal/admission/%s references PhaseHint — admission must never read the hint (Task 92 boundary)", e.Name())
		}
	}
}
