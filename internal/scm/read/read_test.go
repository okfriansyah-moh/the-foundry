package read_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/okfriansyah-moh/the-foundry/internal/scm/read"
)

// initSourceRepo creates a plain (non-bare) repository at dir with one
// commit on "main" and returns its SHA.
func initSourceRepo(t *testing.T, dir string) string {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init source repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add: %v", err)
	}
	sig := &object.Signature{Name: "test", Email: "test@example.com"}
	sha, err := wt.Commit("initial", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), sha)); err != nil {
		t.Fatalf("set main ref: %v", err)
	}
	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := repo.Storer.SetReference(head); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}
	return sha.String()
}

func TestMirrorThenFetchThenResolveRef(t *testing.T) {
	src := t.TempDir()
	wantSHA := initSourceRepo(t, src)

	mirrorPath := filepath.Join(t.TempDir(), "mirror.git")
	ctx := context.Background()

	if err := read.Mirror(ctx, src, mirrorPath); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	got, err := read.ResolveRef(ctx, mirrorPath, "main")
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	if got != wantSHA {
		t.Fatalf("resolved main = %s, want %s", got, wantSHA)
	}

	// Mirror again (existing mirror path) exercises the Fetch branch, not
	// the clone branch.
	if err := read.Mirror(ctx, src, mirrorPath); err != nil {
		t.Fatalf("second mirror (fetch path): %v", err)
	}

	if got, err := read.ResolveRef(ctx, mirrorPath, wantSHA[:12]); err != nil || got != wantSHA {
		t.Fatalf("resolve abbreviated sha: got=%q err=%v, want %s", got, err, wantSHA)
	}
}

func TestResolveRef_UnknownRefIsError(t *testing.T) {
	src := t.TempDir()
	initSourceRepo(t, src)

	if _, err := read.ResolveRef(context.Background(), src, "does-not-exist"); err == nil {
		t.Fatal("resolving an unknown ref must error, got nil")
	}
}

func TestMirror_EmptyArgsAreErrors(t *testing.T) {
	ctx := context.Background()
	if err := read.Mirror(ctx, "", "/tmp/x"); err == nil {
		t.Fatal("empty repoURL must error")
	}
	if err := read.Mirror(ctx, "https://example.invalid/x.git", ""); err == nil {
		t.Fatal("empty mirrorPath must error")
	}
}
