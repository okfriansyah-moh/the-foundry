package kernel_test

import (
	"os"
	"strings"
	"testing"
)

// workflowFiles are the source files whose top-level functions run as
// Temporal workflow code (as opposed to activity code, which is the
// side-effecting boundary and is allowed to call time.Now/rand freely).
// Determinism (Constitution C2) requires workflow code to source time only
// through workflow.Now(ctx) — see workflow.go's package doc.
var workflowFiles = []string{"workflow.go"}

// TestNoTimeNowInWorkflowFiles is the custom lint docs/PLAN.md Task 12's
// Acceptance requires: workflow.go must never call time.Now or math/
// rand — those are the two most common accidental non-determinism
// sources in Temporal workflow code.
func TestNoTimeNowInWorkflowFiles(t *testing.T) {
	for _, name := range workflowFiles {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(raw)
		if strings.Contains(src, "time.Now(") {
			t.Errorf("%s calls time.Now() directly — workflow code must use workflow.Now(ctx) instead", name)
		}
		if strings.Contains(src, "rand.") {
			t.Errorf("%s uses math/rand or crypto/rand directly — workflow code must use workflow.SideEffect instead", name)
		}
	}
}
