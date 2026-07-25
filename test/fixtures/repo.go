// Package fixtures builds throwaway local git repositories for tests that
// need a "canonical" repo to operate against (docs/PLAN.md Task 9 /
// SKP-07). Every repo is created under a t.TempDir(), so no cleanup is
// required by the caller.
package fixtures

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// NewRepo creates a new local git repository in a temp directory with a
// single initial commit on branch "main", and returns its absolute path.
// The repo has no remotes — it is a self-contained fixture for tests
// exercising local worktree/branch operations only.
func NewRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "fixture@example.com")
	runGit(t, dir, "config", "user.name", "Fixture")

	readme := filepath.Join(dir, "README.md")
	writeFile(t, readme, "fixture repo\n")

	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial commit")

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
