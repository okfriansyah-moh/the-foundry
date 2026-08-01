package kernel_test

import (
	"path/filepath"
	"testing"

	"go.temporal.io/sdk/worker"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestReplayRecordedHistories runs worker.WorkflowReplayer against the
// recorded histories under test/histories/ (docs/PLAN.md Task 12 Step 7).
// These histories were captured from real DeliverPlan runs (see
// gen_histories_test.go / TestGenerateHistories) and are replayed here with
// no live Temporal server involved — a code change that breaks
// determinism (e.g. reordering activities, adding a new decision point
// before an existing one) fails this test even though no server is
// running.
func TestReplayRecordedHistories(t *testing.T) {
	for _, name := range []string{"hello_world.json", "failing_task.json", "multi_wave.json"} {
		t.Run(name, func(t *testing.T) {
			replayer := worker.NewWorkflowReplayer()
			replayer.RegisterWorkflow(kernel.DeliverPlan)

			path := filepath.Join("..", "..", "test", "histories", name)
			if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, path); err != nil {
				t.Fatalf("replay %s: %v", path, err)
			}
		})
	}
}
