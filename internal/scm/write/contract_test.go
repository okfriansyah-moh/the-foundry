// docs/PLAN.md Task 62 (TX-09): adapter contract tests — github|bitbucket|localgit.
// Bitbucket tests are gated with RUN_BITBUCKET=1.
package write_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	readpkg "github.com/okfriansyah-moh/the-foundry/internal/scm/read"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

// Backend is the adapter-agnostic contract every SCM backend satisfies.
type Backend interface {
	Mirror(context.Context, string, string) error
	Fetch(context.Context, string) error
	PushBranch(context.Context, write.PushRequest) (write.Receipt, error)
}

type githubBackend struct{ p *write.Pusher }

func (b githubBackend) Mirror(ctx context.Context, repoURL, mirrorPath string) error {
	return readpkg.Mirror(ctx, repoURL, mirrorPath)
}
func (b githubBackend) Fetch(ctx context.Context, mirrorPath string) error {
	return readpkg.Fetch(ctx, mirrorPath)
}
func (b githubBackend) PushBranch(ctx context.Context, req write.PushRequest) (write.Receipt, error) {
	return b.p.PushBranch(ctx, req)
}

type bitbucketBackend struct{ p *write.BitbucketPusher }

func (b bitbucketBackend) Mirror(ctx context.Context, repoURL, mirrorPath string) error {
	return readpkg.BitbucketMirror(ctx, repoURL, mirrorPath)
}
func (b bitbucketBackend) Fetch(ctx context.Context, mirrorPath string) error {
	return readpkg.BitbucketFetch(ctx, mirrorPath)
}
func (b bitbucketBackend) PushBranch(ctx context.Context, req write.PushRequest) (write.Receipt, error) {
	return b.p.PushBranch(ctx, req)
}

type noopLeases struct{}

func (noopLeases) Acquire(context.Context, string, string, time.Duration) (string, error) {
	return "lease-token", nil
}
func (noopLeases) Release(context.Context, string, string) error { return nil }

type memoryLedger struct{ ops map[string]extops.Op }

func (l *memoryLedger) Reserve(_ context.Context, workflowID, kind, target, idempotencyKey string, _ any) (extops.Op, error) {
	if l.ops == nil {
		l.ops = map[string]extops.Op{}
	}
	if op, ok := l.ops[idempotencyKey]; ok {
		return op, nil
	}
	op := extops.Op{ID: extops.OpID(idempotencyKey), WorkflowID: workflowID, Kind: kind, Target: target, IdempotencyKey: idempotencyKey, State: extops.StateReserved}
	l.ops[idempotencyKey] = op
	return op, nil
}
func (l *memoryLedger) MarkExecuted(_ context.Context, id extops.OpID, receipt any) (extops.Op, error) {
	for key, op := range l.ops {
		if op.ID == id {
			op.State = extops.StateExecuted
			l.ops[key] = op
			return op, nil
		}
	}
	return extops.Op{}, errors.New("missing op")
}

type emptyTokenSource struct{}

func (emptyTokenSource) Token(context.Context) (string, error) { return "", nil }

func initRepo(t *testing.T, dir string) string {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
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
		t.Fatalf("set main: %v", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatalf("set head: %v", err)
	}
	return sha.String()
}

func cloneAndCommit(t *testing.T, remote, branch string) (string, string) {
	t.Helper()
	work := t.TempDir()
	repo, err := git.PlainClone(work, false, &git.CloneOptions{URL: remote})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "change.txt"), []byte(branch+"\n"), 0o644); err != nil {
		t.Fatalf("write change: %v", err)
	}
	if _, err := wt.Add("change.txt"); err != nil {
		t.Fatalf("add change: %v", err)
	}
	sig := &object.Signature{Name: "test", Email: "test@example.com"}
	sha, err := wt.Commit("change", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit change: %v", err)
	}
	return work, sha.String()
}

func TestBackendContract_LocalGit(t *testing.T) {
	ctx := context.Background()
	remoteWork := t.TempDir()
	initRepo(t, remoteWork)
	remoteBare := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainClone(remoteBare, true, &git.CloneOptions{URL: remoteWork}); err != nil {
		t.Fatalf("clone bare remote: %v", err)
	}
	mirrorPath := filepath.Join(t.TempDir(), "mirror.git")

	cases := []struct {
		name    string
		backend Backend
		skip    bool
	}{
		{name: "github", backend: githubBackend{p: &write.Pusher{Leases: noopLeases{}, Ledger: &memoryLedger{}, Tokens: emptyTokenSource{}}}},
		{name: "bitbucket", backend: bitbucketBackend{p: &write.BitbucketPusher{Leases: noopLeases{}, Ledger: &memoryLedger{}, Tokens: emptyTokenSource{}}}, skip: os.Getenv("RUN_BITBUCKET") == ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skip("RUN_BITBUCKET=1 not set")
			}
			if err := tc.backend.Mirror(ctx, remoteBare, mirrorPath+tc.name); err != nil {
				t.Fatalf("mirror: %v", err)
			}
			if err := tc.backend.Fetch(ctx, mirrorPath+tc.name); err != nil {
				t.Fatalf("fetch: %v", err)
			}
			work, newSHA := cloneAndCommit(t, remoteBare, tc.name)
			before, err := readpkg.ResolveRef(ctx, remoteBare, "main")
			if err != nil {
				t.Fatalf("resolve before: %v", err)
			}
			receipt, err := tc.backend.PushBranch(ctx, write.PushRequest{
				RepoPath:     work,
				RemoteName:   "origin",
				Branch:       "main",
				ExpectedBase: before,
				NewSHA:       newSHA,
				WorkflowID:   tc.name + "-wf",
			})
			if err != nil {
				t.Fatalf("push: %v", err)
			}
			if receipt.AfterSHA != newSHA {
				t.Fatalf("after_sha=%s want %s", receipt.AfterSHA, newSHA)
			}
			got, err := readpkg.ResolveRef(ctx, remoteBare, "main")
			if err != nil {
				t.Fatalf("resolve after: %v", err)
			}
			if got != newSHA {
				t.Fatalf("remote head=%s want %s", got, newSHA)
			}
		})
	}
}
