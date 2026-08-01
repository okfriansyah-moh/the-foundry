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

// TestPushBranch_RealBitbucket is Task 137's gated real-Bitbucket test.
// Required when RUN_BITBUCKET_LIVE=1:
//
//	BITBUCKET_API_TOKEN
//	SCM_WRITE_TEST_BITBUCKET_REPO_URL
//	SCM_WRITE_TEST_BITBUCKET_BASE_BRANCH
//
// Pushes a disposable branch; never creates a PR (package boundary).
func TestPushBranch_RealBitbucket(t *testing.T) {
	if os.Getenv("RUN_BITBUCKET_LIVE") != "1" {
		t.Skip("RUN_BITBUCKET_LIVE=1 not set — gated real Bitbucket credentials required")
	}

	repoURL := os.Getenv("SCM_WRITE_TEST_BITBUCKET_REPO_URL")
	baseBranch := os.Getenv("SCM_WRITE_TEST_BITBUCKET_BASE_BRANCH")
	if repoURL == "" || baseBranch == "" {
		t.Fatal("RUN_BITBUCKET_LIVE=1 requires SCM_WRITE_TEST_BITBUCKET_REPO_URL and SCM_WRITE_TEST_BITBUCKET_BASE_BRANCH")
	}
	if os.Getenv(write.BitbucketTokenEnvVar) == "" {
		t.Fatalf("RUN_BITBUCKET_LIVE=1 requires %s", write.BitbucketTokenEnvVar)
	}

	ctx := context.Background()
	repoPath := t.TempDir()
	if err := mirrorAndCheckoutBitbucket(ctx, repoURL, repoPath, baseBranch); err != nil {
		t.Fatalf("prepare local clone: %v", err)
	}

	base, err := read.ResolveRef(ctx, repoPath, baseBranch)
	if err != nil {
		t.Fatalf("resolve base branch: %v", err)
	}
	t.Logf("base branch %s currently at %s", baseBranch, base)

	newSHA, err := commitThrowawayFileBitbucket(repoPath)
	if err != nil {
		t.Fatalf("commit throwaway file: %v", err)
	}

	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	branch := fmt.Sprintf("foundry-scm-write-test-%s", hex.EncodeToString(buf))

	pusher := &write.BitbucketPusher{
		Leases: newFakeLeases(),
		Ledger: newFakeLedger(),
		Tokens: write.EnvTokenSource{EnvVar: write.BitbucketTokenEnvVar},
	}
	receipt, err := pusher.PushBranch(ctx, write.PushRequest{
		RepoPath:     repoPath,
		RemoteURL:    repoURL,
		Branch:       branch,
		ExpectedBase: "",
		NewSHA:       newSHA,
		WorkflowID:   "wf-gated-bitbucket-test",
	})
	if err != nil {
		t.Fatalf("PushBranch against real Bitbucket: %v", err)
	}
	t.Logf("pushed %s to %s: %+v (delete this branch manually or via repo cleanup)", newSHA, branch, receipt)

	// Racing commit: push succeeds first, then a second push with stale
	// ExpectedBase must be CAS-rejected (never force-pushed).
	racerSHA, err := commitThrowawayFileBitbucket(repoPath)
	if err != nil {
		t.Fatalf("racer commit: %v", err)
	}
	_, err = pusher.PushBranch(ctx, write.PushRequest{
		RepoPath:     repoPath,
		RemoteURL:    repoURL,
		Branch:       branch,
		ExpectedBase: base, // deliberately stale vs receipt.AfterSHA
		NewSHA:       racerSHA,
		WorkflowID:   "wf-gated-bitbucket-race",
	})
	if err == nil {
		t.Fatal("stale ExpectedBase push succeeded — CAS must reject drift on real Bitbucket")
	}
}

func mirrorAndCheckoutBitbucket(ctx context.Context, repoURL, dest, baseBranch string) error {
	opts := &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(baseBranch),
		SingleBranch:  true,
	}
	if token := os.Getenv(write.BitbucketTokenEnvVar); token != "" {
		opts.Auth = &http.BasicAuth{Username: "x-token-auth", Password: token}
	}
	_, err := git.PlainCloneContext(ctx, dest, false, opts)
	return err
}

func commitThrowawayFileBitbucket(repoPath string) (string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	name := ".foundry-scm-write-gated-bitbucket-test"
	content := fmt.Sprintf("throwaway commit from internal/scm/write's RUN_BITBUCKET_LIVE=1 gated test at %s\n", time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	if _, err := wt.Add(name); err != nil {
		return "", err
	}
	sig := &object.Signature{Name: "foundry-scm-write-gated-test", Email: "noreply@example.invalid", When: time.Now().UTC()}
	h, err := wt.Commit("scm/write gated bitbucket test throwaway commit", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}
