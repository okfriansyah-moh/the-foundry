package write_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/okfriansyah-moh/the-foundry/internal/scm/read"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

// TestPushBranch_RealGitHub is Task 27's optional gated real-GitHub test
// (docs/PLAN.md Task 27 Step 5). It is gated behind RUN_GITHUB=1 and three
// further environment variables identifying a real, disposable sandbox
// repository — it is written to be fully functional, but this
// environment has no GitHub sandbox-org credentials available, so it has
// never been run for real (recorded honestly in docs/PLAN.md Task 27's
// Status line per the no-gaps rule, not silently claimed as passing).
//
// Required environment when RUN_GITHUB=1:
//
//	GITHUB_TOKEN            - a least-privilege PAT (see EnvTokenSource doc
//	                           comment: "repo" classic scope, or a
//	                           fine-grained PAT's "Contents: Read and
//	                           write" on the one target repo only)
//	SCM_WRITE_TEST_GITHUB_REPO_URL   - e.g. https://github.com/some-org/some-sandbox-repo.git
//	SCM_WRITE_TEST_GITHUB_BASE_BRANCH - an existing branch to read the
//	                           current tip of as ExpectedBase (e.g. "main")
//
// The test clones the repo shallowly is not attempted (a real clone is
// simplest and safest here), resolves the base branch's current tip via
// internal/scm/read.ResolveRef, makes one throwaway commit, and pushes it
// to a disposable branch named "foundry-scm-write-test-<random>" — it
// never touches the base branch itself, and never creates a pull request
// (this package has no such API — see doc.go's Boundary paragraph).
func TestPushBranch_RealGitHub(t *testing.T) {
	if os.Getenv("RUN_GITHUB") != "1" {
		t.Skip("RUN_GITHUB=1 not set — this gated test needs real GitHub sandbox-org credentials never available in CI/dev sandboxes by default")
	}

	repoURL := os.Getenv("SCM_WRITE_TEST_GITHUB_REPO_URL")
	baseBranch := os.Getenv("SCM_WRITE_TEST_GITHUB_BASE_BRANCH")
	if repoURL == "" || baseBranch == "" {
		t.Fatal("RUN_GITHUB=1 requires SCM_WRITE_TEST_GITHUB_REPO_URL and SCM_WRITE_TEST_GITHUB_BASE_BRANCH")
	}
	if os.Getenv(write.DefaultTokenEnvVar) == "" {
		t.Fatalf("RUN_GITHUB=1 requires %s (a least-privilege PAT)", write.DefaultTokenEnvVar)
	}

	ctx := context.Background()
	repoPath := t.TempDir()

	// Mirror-then-worktree is Task 27's own read-side machinery
	// (internal/scm/read); reusing it here keeps this gated test honest
	// about which code path it exercises. Import is done lazily via a
	// package-qualified call rather than an unconditional top-level
	// import cycle risk — read has none, this is just locality of
	// reference for a test that virtually never runs.
	if err := mirrorAndCheckout(ctx, repoURL, repoPath, baseBranch); err != nil {
		t.Fatalf("prepare local clone: %v", err)
	}

	base, err := resolveLocalRef(repoPath, baseBranch)
	if err != nil {
		t.Fatalf("resolve base branch: %v", err)
	}
	t.Logf("base branch %s currently at %s", baseBranch, base)

	newSHA, err := commitThrowawayFile(repoPath)
	if err != nil {
		t.Fatalf("commit throwaway file: %v", err)
	}

	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	branch := fmt.Sprintf("foundry-scm-write-test-%s", hex.EncodeToString(buf))

	pusher := &write.Pusher{
		Leases: newFakeLeases(),
		Ledger: newFakeLedger(),
		Tokens: write.EnvTokenSource{},
	}
	receipt, err := pusher.PushBranch(ctx, write.PushRequest{
		RepoPath:     repoPath,
		RemoteURL:    repoURL,
		Branch:       branch,
		ExpectedBase: "", // branch does not exist yet
		NewSHA:       newSHA,
		WorkflowID:   "wf-gated-github-test",
	})
	if err != nil {
		t.Fatalf("PushBranch against real GitHub: %v", err)
	}
	t.Logf("pushed %s to %s: %+v (delete this branch manually or via repo cleanup)", newSHA, branch, receipt)
}

// mirrorAndCheckout clones repoURL's baseBranch into dest as a regular
// (non-bare) checkout, authenticating with GITHUB_TOKEN when set — this
// is the local working copy PushBranch's RepoPath then points at.
func mirrorAndCheckout(ctx context.Context, repoURL, dest, baseBranch string) error {
	opts := &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(baseBranch),
		SingleBranch:  true,
	}
	if token := os.Getenv(write.DefaultTokenEnvVar); token != "" {
		opts.Auth = &http.BasicAuth{Username: "x-access-token", Password: token}
	}
	_, err := git.PlainCloneContext(ctx, dest, false, opts)
	return err
}

// resolveLocalRef delegates to this task's own read-side package
// (internal/scm/read.ResolveRef) so this gated test exercises the same
// read path production code uses, rather than a bespoke lookup.
func resolveLocalRef(repoPath, ref string) (string, error) {
	return read.ResolveRef(context.Background(), repoPath, ref)
}

// commitThrowawayFile adds one small, clearly-marked file to repoPath's
// working tree and commits it, returning the new commit's SHA. It never
// touches any file the target repository's own history already has.
func commitThrowawayFile(repoPath string) (string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	name := ".foundry-scm-write-gated-test"
	content := fmt.Sprintf("throwaway commit from internal/scm/write's RUN_GITHUB=1 gated test at %s\n", time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	if _, err := wt.Add(name); err != nil {
		return "", err
	}
	sig := &object.Signature{Name: "foundry-scm-write-gated-test", Email: "noreply@example.invalid", When: time.Now()}
	h, err := wt.Commit("scm/write gated test throwaway commit", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}
