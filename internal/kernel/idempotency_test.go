package kernel_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

func TestReceiptStore_PutThenGetRoundTrips(t *testing.T) {
	store := kernel.NewMemReceiptStore()
	ctx := context.Background()

	if _, found, err := store.Get(ctx, "k1"); err != nil || found {
		t.Fatalf("get before put: found=%v err=%v, want not found", found, err)
	}
	if err := store.Put(ctx, "k1", []byte(`{"a":1}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	payload, found, err := store.Get(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("get after put: found=%v err=%v", found, err)
	}
	if string(payload) != `{"a":1}` {
		t.Fatalf("payload = %s, want {\"a\":1}", payload)
	}
}

// TestExecuteTask_ReceiptShortCircuitsSecondRun proves the idempotency
// contract from docs/PLAN.md Task 12 Step 5: re-invoking ExecuteTask with
// the same (workflow, task, attempt) key returns the recorded receipt
// instead of re-running the executor adapter — here observed as a second
// call, with the fake script deliberately pointed somewhere nonexistent,
// still returning the first call's exact output.
func TestExecuteTask_ReceiptShortCircuitsSecondRun(t *testing.T) {
	fx := newFixture(t, scriptSuccess)
	ctx := context.Background()

	lease, err := fx.Activities.AcquireLease(ctx, kernel.AcquireLeaseInput{Resource: "r", Holder: "h", TTLSeconds: 60})
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	ws, err := fx.Activities.AcquireWorktree(ctx, kernel.AcquireWorktreeInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1,
		RepoPath: fx.RepoPath, LeaseResource: "r", LeaseToken: lease.Token,
	})
	if err != nil {
		t.Fatalf("acquire worktree: %v", err)
	}

	execIn := kernel.ExecuteTaskInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1,
		ExecutorName: "fake", WorkspacePath: ws.Path,
	}
	execIn.Packet.Goal = fx.ScriptPath

	first, err := fx.Activities.ExecuteTask(ctx, execIn)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}

	// Point Goal at a script that does not exist: a genuine second run
	// would fail to load it. The idempotency key (workflow, task,
	// attempt) is unchanged, so the receipt must short-circuit before the
	// fake executor ever tries to read it.
	execIn.Packet.Goal = "/no/such/script.yaml"
	second, err := fx.Activities.ExecuteTask(ctx, execIn)
	if err != nil {
		t.Fatalf("second execute (should short-circuit via receipt): %v", err)
	}
	if second.Claimed != first.Claimed || second.Failed != first.Failed {
		t.Fatalf("second execute = %+v, want identical receipt %+v", second, first)
	}
}

// TestExecuteTask_WithoutReceiptGuardActuallyReRuns is the negative control
// docs/PLAN.md Task 16 (SKP-14) Step 4 requires: it proves the receipt
// guard above — not luck or timing — is what makes exactly-once real. It
// repeats TestExecuteTask_ReceiptShortCircuitsSecondRun's exact setup
// (same workflow/task/attempt key, Goal repointed at a nonexistent
// script), but the second call goes through a fresh Activities backed by
// an empty ReceiptStore — modeling a receipt row that was deleted (or
// never written) for that key. Without a matching receipt, ExecuteTask
// must actually re-invoke the fake executor, which fails to load the
// nonexistent script — the opposite outcome of the guarded case.
func TestExecuteTask_WithoutReceiptGuardActuallyReRuns(t *testing.T) {
	fx := newFixture(t, scriptSuccess)
	ctx := context.Background()

	lease, err := fx.Activities.AcquireLease(ctx, kernel.AcquireLeaseInput{Resource: "r", Holder: "h", TTLSeconds: 60})
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	ws, err := fx.Activities.AcquireWorktree(ctx, kernel.AcquireWorktreeInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1,
		RepoPath: fx.RepoPath, LeaseResource: "r", LeaseToken: lease.Token,
	})
	if err != nil {
		t.Fatalf("acquire worktree: %v", err)
	}

	execIn := kernel.ExecuteTaskInput{
		WorkflowID: "wf1", TaskID: "t1", Attempt: 1,
		ExecutorName: "fake", WorkspacePath: ws.Path,
	}
	execIn.Packet.Goal = fx.ScriptPath

	if _, err := fx.Activities.ExecuteTask(ctx, execIn); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	// freshActivities shares nothing with fx.Activities except the same
	// ReceiptStore *type* — a brand-new, empty one — so the (workflow,
	// task, attempt) key from the call above has no receipt here. Every
	// other collaborator is nil because ExecuteTask never touches them.
	freshActivities := kernel.NewActivities(nil, nil, nil, nil, kernel.NewMemReceiptStore(), nil, nil, cost.Defaults{}, verify.Runner{})

	execIn.Packet.Goal = "/no/such/script.yaml"
	if _, err := freshActivities.ExecuteTask(ctx, execIn); err == nil {
		t.Fatal("expected a genuine re-run to fail loading the nonexistent script — the receipt guard was what prevented this failure in the guarded test, so its absence here must let the real re-execution happen")
	}
}
