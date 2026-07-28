package contracttest

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/cliexec"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// cleanAdapter is a minimal, correct adapter (per-instance state only). It
// proves leakViolation PASSES a well-behaved adapter.
func cleanAdapter() executor.Adapter {
	return cliexec.New(cliexec.Config{
		Provider:   "clean-fixture",
		Binary:     "true",
		PromptFile: ".clean-fixture-prompt.md",
	})
}

// leakySharedOutput is the shared mutable state that makes leakyAdapter
// violate the fresh-context contract: all instances write to the FIRST
// workspace's prompt path, so a second instance's Prepare contaminates the
// first workspace and leaves the second empty.
var leakySharedOutput atomic.Pointer[string]

type leakyAdapter struct{}

func (a *leakyAdapter) Prepare(_ context.Context, ws worktree.Workspace, packet executor.TaskPacket) error {
	p := filepath.Join(ws.Path, "leaky-prompt.md")
	// CompareAndSwap: only the first instance "wins" and fixes the shared path;
	// subsequent calls ignore the return value intentionally — they still read the
	// FIRST path below, which is what makes this adapter leak (intentional fixture).
	_ = leakySharedOutput.CompareAndSwap(nil, &p)
	target := *leakySharedOutput.Load()
	return os.WriteFile(target, []byte(packet.Goal), 0o600)
}

func (a *leakyAdapter) Run(context.Context) (executor.Summary, error) {
	return executor.Summary{}, nil
}

func (a *leakyAdapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{}, nil
}

// TestContractLeak_ChecksBite proves the fresh-context check both passes a
// clean adapter and FAILS a deliberately-planted leaky one (mirrors Task 18's
// seeded-violation self-test — a check that can't be shown to catch a
// violation isn't trustworthy).
func TestContractLeak_ChecksBite(t *testing.T) {
	if err := leakViolation(cleanAdapter); err != nil {
		t.Fatalf("clean adapter should pass the fresh-context check, got: %v", err)
	}

	leakySharedOutput.Store(nil)
	if err := leakViolation(func() executor.Adapter { return &leakyAdapter{} }); err == nil {
		t.Fatal("leaky adapter passed the fresh-context check — the check does not bite")
	}
}

// TestContractLeak_ConcurrentInstances is the wave-level proof: many adapter
// instances prepared concurrently in independent workspaces show zero
// cross-task leakage (run under -race, this also catches shared-state data
// races). This stands in for the wave e2e — the kernel dispatches wave tasks
// as independent workflows/workspaces, and the isolation guarantee lives in
// the adapter contract proven here.
func TestContractLeak_ConcurrentInstances(t *testing.T) {
	for i := 0; i < 8; i++ {
		if err := leakViolation(cleanAdapter); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}
