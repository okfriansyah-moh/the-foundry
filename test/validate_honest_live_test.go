// docs/PLAN.md Task 104 (SKP-11R2): honest-completion live proof.
//
// Gated behind RUN_VALIDATE_LIVE=1 (mirrors the RUN_RECOVERY_LIVE / RUN_GITHUB
// gated-live-test precedent). It runs the real internal/verify.Runner —
// executing real commands, not testsuite mocks — against the *production*
// config/validation-allowlist.yaml, over three fixtures whose honest terminals
// prove the enforcement point cannot be bypassed:
//
//   - a lying executor whose validation command exits nonzero must classify
//     verification-failed (never a pass);
//   - a non-allowlisted command must classify policy-violation;
//   - a task that declares no validation commands at all must classify
//     no-validation-declared (the omission hole Task 104 closes).
//
// The full real-Temporal-worker DeliverPlan proof and its required CI job run
// against the compose Temporal+Postgres services (see .github/workflows) and
// are exercised there; this gated test proves the honest classification layer
// end-to-end with real command execution locally.
package recoverylive_test

import (
	"context"
	"os"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func gated(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_VALIDATE_LIVE") != "1" {
		t.Skip("set RUN_VALIDATE_LIVE=1 to run the honest-completion live proof")
	}
}

func liveRunner(t *testing.T) verify.Runner {
	t.Helper()
	al, err := verify.LoadAllowlist("../config/validation-allowlist.yaml")
	if err != nil {
		t.Fatalf("load production allowlist: %v", err)
	}
	return verify.NewRunner(al)
}

func TestValidateHonestLive_LyingExecutorFails(t *testing.T) {
	gated(t)
	r := liveRunner(t)
	ws := worktree.Workspace{Path: t.TempDir()}
	// An allowlisted command (go) that nonetheless exits nonzero: the executor
	// may "claim" success, but the records are the only truth.
	records, err := r.Run(context.Background(), ws, []string{"go run ./definitely-not-a-real-package"})
	if err != nil {
		t.Fatalf("runner infra error: %v", err)
	}
	ok, class := verify.Evaluate(records, 1)
	if ok || class != verify.ClassificationVerificationFailed {
		t.Fatalf("lying executor must fail verification-failed; got (%v, %q)", ok, class)
	}
}

func TestValidateHonestLive_NonAllowlistedIsPolicyViolation(t *testing.T) {
	gated(t)
	r := liveRunner(t)
	ws := worktree.Workspace{Path: t.TempDir()}
	records, err := r.Run(context.Background(), ws, []string{"/usr/bin/definitely-not-allowlisted --danger"})
	if err != nil {
		t.Fatalf("runner infra error: %v", err)
	}
	ok, class := verify.Evaluate(records, 1)
	if ok || class != verify.ClassificationPolicyViolation {
		t.Fatalf("non-allowlisted command must be policy-violation; got (%v, %q)", ok, class)
	}
}

func TestValidateHonestLive_NoCommandsIsNoValidationDeclared(t *testing.T) {
	gated(t)
	ok, class := verify.Evaluate(nil, 1)
	if ok || class != verify.ClassificationNoValidationDeclared {
		t.Fatalf("no declared validation must be no-validation-declared; got (%v, %q)", ok, class)
	}
}
