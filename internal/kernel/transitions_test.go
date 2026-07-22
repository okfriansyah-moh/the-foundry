package kernel_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func TestMemTransitionStore_AppendAssignsIncreasingSeq(t *testing.T) {
	store := kernel.NewMemTransitionStore()
	ctx := context.Background()

	seq1, err := store.Append(ctx, "wf1", state.Transition{WorkflowID: "wf1", Status: state.StatusRunning})
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	seq2, err := store.Append(ctx, "wf1", state.Transition{WorkflowID: "wf1", Status: state.StatusSucceeded})
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if seq2 <= seq1 {
		t.Fatalf("seq2 (%d) must be greater than seq1 (%d)", seq2, seq1)
	}

	all := store.All("wf1")
	if len(all) != 2 {
		t.Fatalf("All returned %d transitions, want 2", len(all))
	}

	// A different workflow gets its own independent sequence.
	if _, err := store.Append(ctx, "wf2", state.Transition{WorkflowID: "wf2", Status: state.StatusRunning}); err != nil {
		t.Fatalf("append to wf2: %v", err)
	}
	if got := len(store.All("wf1")); got != 2 {
		t.Fatalf("wf1 transitions changed after appending to wf2: got %d, want 2", got)
	}
}
