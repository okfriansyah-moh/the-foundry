package write_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

// --- fakeLeases: an in-memory LeaseAcquirer, standing in for
// internal/kernel's real lease stores (which this package must never
// import — see doc.go) ---

type fakeLeases struct {
	mu   sync.Mutex
	held map[string]string
}

func newFakeLeases() *fakeLeases { return &fakeLeases{held: make(map[string]string)} }

func (f *fakeLeases) Acquire(_ context.Context, resource, holder string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.held[resource]; ok {
		return existing, nil
	}
	tok := hex.EncodeToString([]byte(holder)) + "-" + time.Now().String()
	f.held[resource] = tok
	return tok, nil
}

func (f *fakeLeases) Release(_ context.Context, resource, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[resource] == token {
		delete(f.held, resource)
	}
	return nil
}

// --- fakeLedger: an in-memory Ledger, isolating these tests from a live
// Postgres — the real internal/ledger/extops.Store is exercised
// separately below (TestPushBranch_RecordsReceiptInRealLedger). ---

type fakeLedger struct {
	mu  sync.Mutex
	ops map[string]*extops.Op
}

func newFakeLedger() *fakeLedger { return &fakeLedger{ops: make(map[string]*extops.Op)} }

func (l *fakeLedger) Reserve(_ context.Context, workflowID, kind, target, idempotencyKey string, request any) (extops.Op, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if op, ok := l.ops[idempotencyKey]; ok {
		return *op, nil
	}
	payload, _ := json.Marshal(request)
	op := &extops.Op{
		ID:             extops.OpID(idempotencyKey),
		WorkflowID:     workflowID,
		Kind:           kind,
		Target:         target,
		IdempotencyKey: idempotencyKey,
		State:          extops.StateReserved,
		Request:        payload,
	}
	l.ops[idempotencyKey] = op
	return *op, nil
}

func (l *fakeLedger) MarkExecuted(_ context.Context, id extops.OpID, receipt any) (extops.Op, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.ops[string(id)]
	if !ok || op.State != extops.StateReserved {
		return extops.Op{}, errors.New("fakeLedger: not reserved")
	}
	payload, _ := json.Marshal(receipt)
	op.State = extops.StateExecuted
	op.Receipt = payload
	return *op, nil
}

// --- fixtures ---

func sig() *object.Signature {
	return &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}
}

// newBareRemote creates a bare repository at dir, standing in for a
// GitHub remote — pushed to over the file:// transport, which go-git
// implements by shelling out to the real git-receive-pack binary (see
// doc.go), so these tests exercise the same server-side ref-update
// enforcement a real remote would apply.
func newBareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInit(dir, true); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}
	return dir
}

// newSourceRepo creates a non-bare repo at a fresh temp dir with "origin"
// pointing at remoteDir, and one commit (not yet pushed) whose SHA it
// returns.
func newSourceRepo(t *testing.T, remoteDir string) (repoPath, sha string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init source: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteDir}}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	h, err := wt.Commit("initial", &git.CommitOptions{Author: sig(), Committer: sig()})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir, h.String()
}

// cloneSibling clones sourceRepoPath's current HEAD into a fresh temp
// directory and repoints its "origin" remote at remoteDir (the shared
// fixture remote) instead of sourceRepoPath itself — used to simulate a
// second, independent actor racing against the caller's own repo clone.
func cloneSibling(t *testing.T, sourceRepoPath, remoteDir string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainClone(dir, false, &git.CloneOptions{URL: sourceRepoPath}); err != nil {
		t.Fatalf("clone sibling from %s: %v", sourceRepoPath, err)
	}
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open sibling clone: %v", err)
	}
	if err := repo.DeleteRemote("origin"); err != nil {
		t.Fatalf("delete sibling's origin: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteDir}}); err != nil {
		t.Fatalf("repoint sibling's origin: %v", err)
	}
	return dir
}

// pushDirect pushes sha as branch's tip straight to remoteDir using go-git
// directly (bypassing internal/scm/write entirely) — used to bootstrap a
// remote's initial state and to simulate a racing commit made by someone
// else.
func pushDirect(t *testing.T, repoPath, branch, sha string) {
	t.Helper()
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("open %s: %v", repoPath, err)
	}
	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(sha + ":refs/heads/" + branch)},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		t.Fatalf("bootstrap push %s to %s: %v", sha, branch, err)
	}
}

// commitChild adds a new commit as a child of the worktree's current HEAD
// and returns its SHA, without pushing it anywhere.
func commitChild(t *testing.T, repoPath, content string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("open %s: %v", repoPath, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	h, err := wt.Commit("child", &git.CommitOptions{Author: sig(), Committer: sig()})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return h.String()
}

// remoteBranchSHA reads branch's current tip directly out of the bare
// remote at remoteDir, independent of anything internal/scm/write did.
func remoteBranchSHA(t *testing.T, remoteDir, branch string) string {
	t.Helper()
	repo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote %s: %v", remoteDir, err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatalf("read remote ref %s: %v", branch, err)
	}
	return ref.Hash().String()
}

func newPusher() (*write.Pusher, *fakeLeases, *fakeLedger) {
	leases := newFakeLeases()
	ledger := newFakeLedger()
	return &write.Pusher{
		Leases: leases,
		Ledger: ledger,
		Tokens: write.EnvTokenSource{EnvVar: "SCM_WRITE_TEST_UNUSED_TOKEN"},
	}, leases, ledger
}

func TestPushBranch_Success(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)

	newSHA := commitChild(t, repoPath, "v2\n")

	pusher, leases, _ := newPusher()
	ctx := context.Background()

	receipt, err := pusher.PushBranch(ctx, write.PushRequest{
		RepoPath:     repoPath,
		Branch:       "main",
		ExpectedBase: base,
		NewSHA:       newSHA,
		WorkflowID:   "wf-1",
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

	// The lease taken for this push must have been released, not left
	// held, once PushBranch returns.
	leases.mu.Lock()
	_, stillHeld := leases.held["scm-push:"+remoteDir+":main"]
	leases.mu.Unlock()
	if stillHeld {
		t.Fatal("lease was not released after a successful push")
	}
}

func TestPushBranch_NewBranchExpectedBaseEmpty(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, newSHA := newSourceRepo(t, remoteDir)

	pusher, _, _ := newPusher()
	receipt, err := pusher.PushBranch(context.Background(), write.PushRequest{
		RepoPath:     repoPath,
		Branch:       "foundry/e2e/new-branch",
		ExpectedBase: "",
		NewSHA:       newSHA,
		WorkflowID:   "wf-1",
	})
	if err != nil {
		t.Fatalf("PushBranch (new branch): %v", err)
	}
	if receipt.AfterSHA != newSHA {
		t.Fatalf("receipt.AfterSHA = %s, want %s", receipt.AfterSHA, newSHA)
	}
	if got := remoteBranchSHA(t, remoteDir, "foundry/e2e/new-branch"); got != newSHA {
		t.Fatalf("remote branch tip = %s, want %s", got, newSHA)
	}
}

// TestPushBranch_CASRejectsDrift is the crux of Task 27's credibility: it
// seeds a racing commit on the remote strictly AFTER expectedBase was
// established and BEFORE PushBranch is ever called, and proves the push
// is rejected — server-side (go-git's file:// transport shells out to the
// real git-receive-pack binary, so this is the same enforcement a real
// GitHub remote applies, not a client-side comparison against a value
// read earlier; see doc.go).
func TestPushBranch_CASRejectsDrift(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)

	// The racer clones repoPath while it is still sitting at base (before
	// our own commitChild below), so the racer's commit is a true sibling
	// of ours — both children of the exact same base — not merely an
	// unrelated history that a plain fast-forward check would already
	// reject on its own. This is the realistic race Task 27 describes: a
	// second push lands on the same branch between us resolving
	// expectedBase and us actually pushing.
	racerRepoPath := cloneSibling(t, repoPath, remoteDir)
	racerSHA := commitChild(t, racerRepoPath, "theirs\n")
	pushDirect(t, racerRepoPath, "main", racerSHA)

	if got := remoteBranchSHA(t, remoteDir, "main"); got != racerSHA {
		t.Fatalf("setup: remote tip = %s, want racer's %s", got, racerSHA)
	}

	// Only now do we build our own commit — repoPath's HEAD is still
	// base, so newSHA is base's other child: a sibling of racerSHA, not a
	// descendant of it.
	newSHA := commitChild(t, repoPath, "ours\n")

	pusher, leases, ledger := newPusher()
	ctx := context.Background()

	_, err := pusher.PushBranch(ctx, write.PushRequest{
		RepoPath:     repoPath,
		Branch:       "main",
		ExpectedBase: base, // stale — the remote has already moved to racerSHA
		NewSHA:       newSHA,
		WorkflowID:   "wf-1",
	})
	if err == nil {
		t.Fatal("PushBranch succeeded despite a racing commit on the remote — CAS did not reject drift")
	}

	if got := remoteBranchSHA(t, remoteDir, "main"); got != racerSHA {
		t.Fatalf("remote tip changed to %s after a rejected push — want unchanged %s", got, racerSHA)
	}

	// A rejected push must not leave a phantom "executed" ledger receipt,
	// and must release its lease so a corrected retry can proceed.
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

func TestPushBranch_IdempotentReplayReturnsSameReceiptWithoutRePushing(t *testing.T) {
	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)
	newSHA := commitChild(t, repoPath, "v2\n")

	pusher, _, _ := newPusher()
	ctx := context.Background()
	req := write.PushRequest{
		RepoPath:       repoPath,
		Branch:         "main",
		ExpectedBase:   base,
		NewSHA:         newSHA,
		WorkflowID:     "wf-1",
		IdempotencyKey: "fixed-key-1",
	}

	first, err := pusher.PushBranch(ctx, req)
	if err != nil {
		t.Fatalf("first push: %v", err)
	}

	// A second call with the same idempotency key must not attempt to
	// push again (which would now fail non-fast-forward since the remote
	// already advanced to newSHA and req.ExpectedBase is stale) — it must
	// short-circuit to the recorded receipt instead.
	second, err := pusher.PushBranch(ctx, req)
	if err != nil {
		t.Fatalf("replayed push with same idempotency key errored: %v", err)
	}
	if second != first {
		t.Fatalf("replayed push returned a different receipt: %+v != %+v", second, first)
	}
}

func TestPushBranch_RejectsForceableInputs(t *testing.T) {
	pusher, _, _ := newPusher()
	ctx := context.Background()

	cases := []write.PushRequest{
		{RepoPath: "x", Branch: "main; rm -rf /", NewSHA: strings.Repeat("a", 40)},
		{RepoPath: "x", Branch: "+refs/heads/main", NewSHA: strings.Repeat("a", 40)},
		{RepoPath: "x", Branch: "main", NewSHA: "not-a-sha"},
		{RepoPath: "x", Branch: "main", NewSHA: strings.Repeat("a", 40), ExpectedBase: "not-a-sha"},
		{RepoPath: "x", Branch: "a/../b", NewSHA: strings.Repeat("a", 40)},
	}
	for _, tc := range cases {
		if _, err := pusher.PushBranch(ctx, tc); err == nil {
			t.Fatalf("request %+v was accepted, want validation error", tc)
		}
	}
}

// TestPushBranch_RecordsReceiptInRealLedger proves real integration with
// Task 26's Postgres-backed ledger (internal/ledger/extops.Store), not
// just this file's fakeLedger. Skips without a Postgres DSN, matching
// internal/ledger/extops's own openTestDB precedent — run for real via
// `make test`/`docker compose run --rm dev go test ./internal/scm/...`.
func TestPushBranch_RecordsReceiptInRealLedger(t *testing.T) {
	dsn := os.Getenv("EXTOPS_TEST_PG_DSN")
	if dsn == "" {
		dsn = os.Getenv("PG_DSN")
	}
	if dsn == "" {
		t.Skip("EXTOPS_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/scm/...` for a real Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	const ddl = `
CREATE TABLE IF NOT EXISTS external_operations (
    id              TEXT PRIMARY KEY,
    workflow_id     TEXT NOT NULL,
    kind            TEXT NOT NULL,
    target          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    state           TEXT NOT NULL CHECK (state IN ('reserved', 'executed', 'reconciled', 'failed')),
    request         JSONB NOT NULL,
    receipt         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("ensure external_operations exists: %v", err)
	}

	remoteDir := newBareRemote(t)
	repoPath, base := newSourceRepo(t, remoteDir)
	pushDirect(t, repoPath, "main", base)
	newSHA := commitChild(t, repoPath, "real-ledger\n")

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random key: %v", err)
	}
	key := "scm-write-real-ledger-" + hex.EncodeToString(buf)

	pusher := &write.Pusher{
		Leases: newFakeLeases(),
		Ledger: extops.NewStore(db),
		Tokens: write.EnvTokenSource{EnvVar: "SCM_WRITE_TEST_UNUSED_TOKEN"},
	}

	receipt, err := pusher.PushBranch(context.Background(), write.PushRequest{
		RepoPath:       repoPath,
		Branch:         "main",
		ExpectedBase:   base,
		NewSHA:         newSHA,
		WorkflowID:     "wf-real-ledger",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("PushBranch: %v", err)
	}

	var state, receiptJSON string
	if err := db.QueryRow(`SELECT state, receipt FROM external_operations WHERE idempotency_key = $1`, key).Scan(&state, &receiptJSON); err != nil {
		t.Fatalf("query ledger row: %v", err)
	}
	if state != string(extops.StateExecuted) {
		t.Fatalf("ledger state = %q, want executed", state)
	}
	var stored write.Receipt
	if err := json.Unmarshal([]byte(receiptJSON), &stored); err != nil {
		t.Fatalf("unmarshal stored receipt: %v", err)
	}
	if stored != receipt {
		t.Fatalf("stored receipt %+v != returned receipt %+v", stored, receipt)
	}
}
