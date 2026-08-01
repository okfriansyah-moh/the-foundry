package mission

import (
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// TestGenerateImprovementPlan_RealValidationNoWildcard proves docs/PLAN.md Task
// 127: the improvement plan is produced by the real generator, so it carries
// real per-task validation (an allowlisted command or an explicit opt-out with
// a reason — never a hollow `make test`) and no wildcard repo-write permission.
func TestGenerateImprovementPlan_RealValidationNoWildcard(t *testing.T) {
	mc := spec.MissionContext{
		RepoAlias:       "product",
		RepoURL:         "https://example.invalid/products/demo",
		RepoBranch:      "main",
		RepoWriteTarget: "products/demo",
	}
	doc, err := GenerateImprovementPlan("m1", "demo", "raise activation on the onboarding page", spec.EffectMapping{}, mc, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("GenerateImprovementPlan: %v", err)
	}
	if doc == nil || len(doc.Tasks) == 0 {
		t.Fatalf("generated plan has no tasks: %+v", doc)
	}
	if !strings.HasPrefix(doc.ID, "improve-") {
		t.Fatalf("plan ID %q must keep the improve- prefix", doc.ID)
	}
	// No requested permission may target a wildcard.
	for _, p := range doc.RequestedPermissions {
		if p.Target == "*" {
			t.Fatalf("improvement plan must not request a wildcard permission, got %+v", p)
		}
	}
	// Every task must carry real validation: either an allowlisted validation
	// command, or an explicit opt-out with a reason (never a silent empty set,
	// never a hollow `make test`).
	for _, task := range doc.Tasks {
		for _, vc := range task.ValidationCommands {
			if strings.TrimSpace(vc) == "make test" {
				t.Fatalf("task %s uses a hollow `make test` validation — Task 127 forbids it", task.ID)
			}
		}
		hasValidation := len(task.ValidationCommands) > 0 || task.ValidationOptOutReason != ""
		if !hasValidation {
			t.Fatalf("task %s has neither validation commands nor an opt-out reason", task.ID)
		}
		for _, f := range task.Files {
			if strings.Contains(f, "**") {
				t.Fatalf("task %s uses a wildcard file glob %q — least-privilege violated", task.ID, f)
			}
		}
	}
}

// TestPlanDocFromSpec_DelegatesToGenerator proves the legacy helper now returns
// a real least-privilege plan (or nil), never the old hand-built wildcard plan.
func TestPlanDocFromSpec_DelegatesToGenerator(t *testing.T) {
	doc := PlanDocFromSpec("m1", "demo", "improve retention", time.Unix(1_700_000_000, 0))
	if doc == nil {
		t.Skip("PlanDocFromSpec returned nil (no valid repo context) — acceptable; callers should use GenerateImprovementPlan with real MissionContext")
	}
	for _, task := range doc.Tasks {
		for _, f := range task.Files {
			if strings.Contains(f, "**") {
				t.Fatalf("PlanDocFromSpec still emits a wildcard glob %q", f)
			}
		}
	}
}
