package kernel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

func TestMemLeaseStore_AcquireIdempotentForSameHolder(t *testing.T) {
	store := kernel.NewMemLeaseStore()
	ctx := context.Background()

	first, err := store.Acquire(ctx, "worktree:wf1:t1", "wf1", time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	second, err := store.Acquire(ctx, "worktree:wf1:t1", "wf1", time.Minute)
	if err != nil {
		t.Fatalf("re-acquire same holder: %v", err)
	}
	if first.Token != second.Token {
		t.Fatalf("token changed across re-acquire by the same holder: %s != %s", first.Token, second.Token)
	}
}

func TestMemLeaseStore_ConflictingHolderIsErrLeaseHeld(t *testing.T) {
	store := kernel.NewMemLeaseStore()
	ctx := context.Background()

	if _, err := store.Acquire(ctx, "worktree:wf1:t1", "wf1", time.Minute); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, err := store.Acquire(ctx, "worktree:wf1:t1", "wf2", time.Minute)
	if !errors.Is(err, kernel.ErrLeaseHeld) {
		t.Fatalf("acquire by a different holder: got %v, want ErrLeaseHeld", err)
	}
}

func TestMemLeaseStore_Check(t *testing.T) {
	store := kernel.NewMemLeaseStore()
	ctx := context.Background()

	lease, err := store.Acquire(ctx, "worktree:wf1:t1", "wf1", time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	ok, err := store.Check(ctx, "worktree:wf1:t1", lease.Token)
	if err != nil || !ok {
		t.Fatalf("check valid token: ok=%v err=%v", ok, err)
	}

	ok, err = store.Check(ctx, "worktree:wf1:t1", "not-the-token")
	if err != nil || ok {
		t.Fatalf("check wrong token: ok=%v err=%v, want false", ok, err)
	}

	ok, err = store.Check(ctx, "no-such-resource", "anything")
	if err != nil || ok {
		t.Fatalf("check unknown resource: ok=%v err=%v, want false", ok, err)
	}
}
