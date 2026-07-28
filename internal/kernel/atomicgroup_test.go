package kernel_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestAtomicGroup_ManifestDigest_Deterministic verifies the digest is stable
// regardless of file/test insertion order.
func TestAtomicGroup_ManifestDigest_Deterministic(t *testing.T) {
	cs1 := kernel.ChangeSet{
		Files: []kernel.FileChange{
			{Path: "b/file.go", Action: kernel.FileActionAdded},
			{Path: "a/file.go", Action: kernel.FileActionModified},
		},
		Tests: []string{"make lint", "make test"},
	}
	cs2 := kernel.ChangeSet{
		Files: []kernel.FileChange{
			{Path: "a/file.go", Action: kernel.FileActionModified},
			{Path: "b/file.go", Action: kernel.FileActionAdded},
		},
		Tests: []string{"make test", "make lint"},
	}
	if cs1.Digest() != cs2.Digest() {
		t.Errorf("digest not deterministic under reordering: %q != %q", cs1.Digest(), cs2.Digest())
	}
}

// TestAtomicGroup_TipCommitTrailer verifies the trailer format.
func TestAtomicGroup_TipCommitTrailer(t *testing.T) {
	g := kernel.AtomicGroup{
		ID:          "group-1",
		PlanTaskIDs: []string{"t1"},
		Commits:     []string{"abc123"},
		Manifest: kernel.ChangeSet{
			Files: []kernel.FileChange{{Path: "src/main.go", Action: kernel.FileActionModified}},
		},
	}
	trailer := g.TipCommitTrailer()
	const prefix = "Foundry-Changeset: "
	if len(trailer) <= len(prefix) {
		t.Fatalf("trailer too short: %q", trailer)
	}
	if trailer[:len(prefix)] != prefix {
		t.Errorf("trailer prefix %q, want %q", trailer[:len(prefix)], prefix)
	}
}

// TestAtomicGroup_ScopeGuard_InScope verifies in-scope file passes.
func TestAtomicGroup_ScopeGuard_InScope(t *testing.T) {
	g := kernel.AtomicGroup{
		ID:          "group-1",
		PlanTaskIDs: []string{"t1"},
		Manifest: kernel.ChangeSet{
			Files: []kernel.FileChange{
				{Path: "internal/mission/improve.go", Action: kernel.FileActionAdded},
			},
		},
	}
	scope := kernel.DeclaredScope{Prefixes: []string{"internal/mission/**"}}
	if err := g.ValidateScope(scope); err != nil {
		t.Errorf("unexpected scope violation: %v", err)
	}
}

// TestAtomicGroup_ScopeGuard_OutOfScope verifies out-of-scope file fails.
func TestAtomicGroup_ScopeGuard_OutOfScope(t *testing.T) {
	g := kernel.AtomicGroup{
		ID:          "group-1",
		PlanTaskIDs: []string{"t1"},
		Manifest: kernel.ChangeSet{
			Files: []kernel.FileChange{
				{Path: "internal/mission/improve.go", Action: kernel.FileActionAdded},
				{Path: "internal/kernel/workflow.go", Action: kernel.FileActionModified}, // not in scope
			},
		},
	}
	scope := kernel.DeclaredScope{Prefixes: []string{"internal/mission/**"}}
	err := g.ValidateScope(scope)
	if err == nil {
		t.Fatal("expected scope violation, got nil")
	}
	scopeErr, ok := err.(*kernel.ScopeViolationError)
	if !ok {
		t.Fatalf("error type %T, want *kernel.ScopeViolationError", err)
	}
	if scopeErr.Path != "internal/kernel/workflow.go" {
		t.Errorf("Path=%q, want internal/kernel/workflow.go", scopeErr.Path)
	}
}

// TestAtomicGroup_DigestChangesWithContent verifies different content → different digest.
func TestAtomicGroup_DigestChangesWithContent(t *testing.T) {
	cs1 := kernel.ChangeSet{Files: []kernel.FileChange{{Path: "a.go", Action: kernel.FileActionAdded}}}
	cs2 := kernel.ChangeSet{Files: []kernel.FileChange{{Path: "b.go", Action: kernel.FileActionAdded}}}
	if cs1.Digest() == cs2.Digest() {
		t.Error("different change sets produced same digest")
	}
}
