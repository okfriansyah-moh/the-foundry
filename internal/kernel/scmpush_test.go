package kernel_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

// fakeLedger is a minimal in-memory kernel.ExternalOpStore, standing in
// for internal/ledger/extops.Store (Task 26's real Postgres-backed
// ledger, already exercised directly by internal/scm/write's own tests
// and, for real, by `make e2e-github`). It exists only to prove
// kernel.PushBranch's adapter wiring is correct end to end.
type fakeLedger struct {
	ops map[string]*extops.Op
}

func newFakeLedger() *fakeLedger { return &fakeLedger{ops: make(map[string]*extops.Op)} }

func (l *fakeLedger) Reserve(_ context.Context, workflowID, kind, target, idempotencyKey string, _ any) (extops.Op, error) {
	if op, ok := l.ops[idempotencyKey]; ok {
		return *op, nil
	}
	op := &extops.Op{ID: extops.OpID(idempotencyKey), WorkflowID: workflowID, Kind: kind, Target: target, IdempotencyKey: idempotencyKey, State: extops.StateReserved}
	l.ops[idempotencyKey] = op
	return *op, nil
}

func (l *fakeLedger) MarkExecuted(_ context.Context, id extops.OpID, receipt any) (extops.Op, error) {
	op := l.ops[string(id)]
	op.State = extops.StateExecuted
	b, _ := json.Marshal(receipt)
	op.Receipt = b
	return *op, nil
}

func TestPushBranch_KernelWiring(t *testing.T) {
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInit(remoteDir, true); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}

	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteDir}}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	sig := &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}
	newSHA, err := wt.Commit("initial", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	leases := kernel.NewMemLeaseStore()
	ledger := newFakeLedger()

	receipt, err := kernel.PushBranch(context.Background(), leases, ledger, write.EnvTokenSource{}, write.PushRequest{
		RepoPath:     repoDir,
		Branch:       "foundry/e2e/kernel-wiring-test",
		ExpectedBase: "",
		NewSHA:       newSHA.String(),
		WorkflowID:   "wf-kernel-wiring",
	})
	if err != nil {
		t.Fatalf("kernel.PushBranch: %v", err)
	}
	if receipt.AfterSHA != newSHA.String() {
		t.Fatalf("receipt.AfterSHA = %s, want %s", receipt.AfterSHA, newSHA.String())
	}

	// The lease must be released, not left held, after a successful push
	// — proving leaseAdapter's Release call actually reaches the real
	// kernel.LeaseStore, not just Acquire.
	ok, err := leases.Check(context.Background(), "scm-push:"+remoteDir+":foundry/e2e/kernel-wiring-test", "irrelevant-since-released")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if ok {
		t.Fatal("lease unexpectedly reports held after PushBranch returned")
	}
}
