package write_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

func newBitbucketPusher() (*write.BitbucketPusher, *fakeLeases, *fakeLedger) {
	leases := newFakeLeases()
	ledger := newFakeLedger()
	return &write.BitbucketPusher{
		Leases: leases,
		Ledger: ledger,
		Tokens: write.EnvTokenSource{EnvVar: "SCM_WRITE_TEST_UNUSED_BITBUCKET_TOKEN"},
	}, leases, ledger
}

func TestBitbucketPushBranch_Success(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)

	newSHA := commitChild(t, repoPath, "v2\n")

	pusher, leases, _ := newBitbucketPusher()
	ctx := context.Background()

	receipt, err := pusher.PushBranch(ctx, write.PushRequest{
		RepoPath:     repoPath,
		Branch:       "main",
		ExpectedBase: base,
		NewSHA:       newSHA,
		WorkflowID:   "wf-bb-1",
	})
	if err != nil {
		t.Fatalf("PushBranch: %v", err)
	}
	if receipt.BeforeSHA != base || receipt.AfterSHA != newSHA {
		t.Fatalf("receipt = %+v, want before=%s after=%s", receipt, base, newSHA)
	}
	if got := remoteBranchSHA(t, remoteDir, "main"); got != newSHA {
		t.Fatalf("remote branch tip = %s, want %s", got, newSHA)
	}

	leases.mu.Lock()
	_, stillHeld := leases.held["scm-push:"+remoteDir+":main"]
	leases.mu.Unlock()
	if stillHeld {
		t.Fatal("lease was not released after a successful push")
	}
}

func TestBitbucketPushBranch_CASRejectsDrift(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)

	racerRepoPath := cloneSibling(t, repoPath, remoteDir)
	racerSHA := commitChild(t, racerRepoPath, "theirs\n")
	pushDirect(t, racerRepoPath, "main", racerSHA)

	if got := remoteBranchSHA(t, remoteDir, "main"); got != racerSHA {
		t.Fatalf("setup: remote tip = %s, want racer's %s", got, racerSHA)
	}

	newSHA := commitChild(t, repoPath, "ours\n")

	pusher, leases, ledger := newBitbucketPusher()
	_, err := pusher.PushBranch(context.Background(), write.PushRequest{
		RepoPath:     repoPath,
		Branch:       "main",
		ExpectedBase: base,
		NewSHA:       newSHA,
		WorkflowID:   "wf-bb-1",
	})
	if err == nil {
		t.Fatal("PushBranch succeeded despite a racing commit on the remote — CAS did not reject drift")
	}

	if got := remoteBranchSHA(t, remoteDir, "main"); got != racerSHA {
		t.Fatalf("remote tip changed to %s after a rejected push — want unchanged %s", got, racerSHA)
	}

	ledger.mu.Lock()
	for _, op := range ledger.ops {
		if op.State == extops.StateExecuted {
			ledger.mu.Unlock()
			t.Fatalf("op %s recorded executed despite a rejected push", op.ID)
		}
	}
	ledger.mu.Unlock()

	leases.mu.Lock()
	_, stillHeld := leases.held["scm-push:"+remoteDir+":main"]
	leases.mu.Unlock()
	if stillHeld {
		t.Fatal("lease was not released after a rejected push")
	}
}

func TestBitbucketPushBranch_IdempotentReplay(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)
	newSHA := commitChild(t, repoPath, "v2\n")

	pusher, _, _ := newBitbucketPusher()
	ctx := context.Background()
	req := write.PushRequest{
		RepoPath:       repoPath,
		Branch:         "main",
		ExpectedBase:   base,
		NewSHA:         newSHA,
		WorkflowID:     "wf-bb-1",
		IdempotencyKey: "bb-fixed-key-1",
	}

	first, err := pusher.PushBranch(ctx, req)
	if err != nil {
		t.Fatalf("first push: %v", err)
	}
	second, err := pusher.PushBranch(ctx, req)
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if first != second {
		t.Fatalf("replay receipt mismatch: first=%+v second=%+v", first, second)
	}
	if got := remoteBranchSHA(t, remoteDir, "main"); got != newSHA {
		t.Fatalf("remote tip = %s, want %s", got, newSHA)
	}
}

func TestBitbucketAuthUsername(t *testing.T) {
	auth := write.AuthForTest(write.ProviderBitbucket, "https://bitbucket.org/ws/repo.git", "tok")
	if auth == nil {
		t.Fatal("expected auth")
	}
	if auth.Username != "x-token-auth" {
		t.Fatalf("username = %q, want x-token-auth", auth.Username)
	}
	if auth.Password != "tok" {
		t.Fatalf("password leaked/mismatched")
	}

	gh := write.AuthForTest(write.ProviderGitHub, "https://github.com/o/r.git", "tok")
	if gh == nil || gh.Username != "x-access-token" {
		t.Fatalf("github username = %v, want x-access-token", gh)
	}

	if write.AuthForTest(write.Provider("unknown"), "https://example.com/r.git", "tok") != nil {
		t.Fatal("unknown provider must refuse auth rather than guess a username")
	}
	if write.AuthForTest(write.ProviderBitbucket, "file:///tmp/r.git", "tok") != nil {
		t.Fatal("file:// remotes need no HTTP auth")
	}
}

func TestBitbucketTokenConstants(t *testing.T) {
	if write.BitbucketTokenEnvVar != "BITBUCKET_API_TOKEN" {
		t.Fatalf("BitbucketTokenEnvVar = %q", write.BitbucketTokenEnvVar)
	}
	if write.DefaultBitbucketTokenSecretName != "bitbucket_token" {
		t.Fatalf("DefaultBitbucketTokenSecretName = %q", write.DefaultBitbucketTokenSecretName)
	}
	if write.DefaultTokenEnvVar != "GITHUB_TOKEN" {
		t.Fatalf("DefaultTokenEnvVar changed: %q", write.DefaultTokenEnvVar)
	}
	if write.DefaultTokenSecretName != "github_token" {
		t.Fatalf("DefaultTokenSecretName changed: %q", write.DefaultTokenSecretName)
	}
}

func TestSandboxAllowlistExcludesBitbucket(t *testing.T) {
	// SCM writes are kernel-owned (C4); bitbucket.org must never appear on
	// the executor sandbox egress allowlist (Task 137 step 6).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "config", "sandbox-egress-allowlist.yaml"))
	if err != nil {
		t.Fatalf("read allowlist: %v", err)
	}
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "bitbucket.org") {
		t.Fatal("config/sandbox-egress-allowlist.yaml must not list bitbucket.org — SCM writes are kernel-side")
	}
	if strings.Contains(lower, "github.com") {
		t.Fatal("config/sandbox-egress-allowlist.yaml must not list github.com — SCM writes are kernel-side")
	}
}
