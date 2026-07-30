package kernel_test

import (
	"os"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestPushCadenceContradictionRegression is docs/PLAN.md Task 108's seeded
// regression guard: it fails if the workflow default, ten-x-branch.md or
// multi-repository.md drifts back to a contradictory push-cadence rule. The
// single canonical rule is: default after-atomic-group; after-accepted-task
// only under intermediate_branch_invariant: buildable-and-testable.
func TestPushCadenceContradictionRegression(t *testing.T) {
	if kernel.DefaultPushCadence != kernel.CadenceAfterAtomicGroup {
		t.Fatalf("workflow default cadence drifted to %q", kernel.DefaultPushCadence)
	}

	read := func(p string) string {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		return string(raw)
	}
	tenx := read("../../docs/foundry/docs/workflows/ten-x-branch.md")
	multi := read("../../docs/foundry/docs/workflows/multi-repository.md")

	for name, doc := range map[string]string{"ten-x-branch.md": tenx, "multi-repository.md": multi} {
		if !strings.Contains(doc, "push_cadence: after-atomic-group") {
			t.Fatalf("%s no longer names after-atomic-group as the canonical default", name)
		}
		if !strings.Contains(doc, kernel.InvariantBuildableAndTestable) {
			t.Fatalf("%s no longer guards after-accepted-task with the buildable-and-testable invariant", name)
		}
	}

	// The kernel validator must still enforce the rule.
	if err := kernel.ValidatePushCadence(kernel.CadenceAfterAcceptedTask, ""); err == nil {
		t.Fatal("ValidatePushCadence must refuse after-accepted-task without the invariant")
	}
}
