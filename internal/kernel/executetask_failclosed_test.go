package kernel_test

import (
	"context"
	"testing"

	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// TestExecuteTask_EmptyAllowlistFailsClosed proves Task 116 (SEC-02): a task
// dispatched with an absent or empty executor allowlist is REFUSED with a
// policy-violation classification, never an unchecked host lookup.
func TestExecuteTask_EmptyAllowlistFailsClosed(t *testing.T) {
	acts := kernel.NewActivities(nil, nil, nil, nil, kernel.NewMemReceiptStore(), nil, nil, cost.Defaults{}, verify.Runner{})

	for _, tc := range []struct {
		name  string
		allow []string
	}{
		{"nil allowlist", nil},
		{"empty allowlist", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := acts.ExecuteTask(context.Background(), kernel.ExecuteTaskInput{
				WorkflowID: "wf1", TaskID: "t1", Attempt: 1,
				ExecutorName: "fake", WorkspacePath: t.TempDir(),
				ExecutorAllowlist: tc.allow,
			})
			if err != nil {
				t.Fatalf("ExecuteTask returned a Go error, want a data refusal: %v", err)
			}
			if !out.Failed {
				t.Fatal("an absent/empty allowlist must refuse (Failed=true)")
			}
			if out.Classification != string(verify.ClassificationPolicyViolation) {
				t.Fatalf("want policy-violation classification, got %q", out.Classification)
			}
		})
	}
}
