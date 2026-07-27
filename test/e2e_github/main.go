// Command e2e_github is `make e2e-github`'s driver (docs/PLAN.md Task 27 /
// FND-08 Step 6). As named, "e2e-github" suggests a real GitHub remote;
// this environment has no GitHub sandbox-org credentials, so — following
// this task's own local-fixture-first approach (its integration tests
// already push against a real local bare git repo rather than a live
// GitHub remote) — this program substitutes a local bare-repo "fixture
// remote" for GitHub. That substitution is recorded honestly in
// docs/PLAN.md Task 27's Status line, not silently glossed over.
//
// It exercises the real, production kernel.PushBranch entry point (the
// same function a future Activities.PushBranch would wrap) against a real
// Postgres-backed lease store and extops ledger, proving: the kernel-only
// push protocol lands a brand-new branch on a fixture remote, and records
// a real receipt to Task 26's ledger — not just internal/scm/write's own
// package tests, which use in-memory fakes for the lease/ledger seam.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/read"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("e2e_github: %v", err)
	}
}

func run() error {
	ctx := context.Background()

	pgDSN := envOr("PG_DSN", "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable")
	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres (is `make up` running?): %w", err)
	}
	if err := ensureSchema(ctx, db); err != nil {
		return err
	}

	workdir, err := os.MkdirTemp("", "foundry-e2e-github-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(workdir)

	fixtureRemote := filepath.Join(workdir, "fixture-remote.git")
	if _, err := git.PlainInit(fixtureRemote, true); err != nil {
		return fmt.Errorf("init fixture remote: %w", err)
	}
	fmt.Printf("fixture remote (substituting for GitHub): %s\n", fixtureRemote)

	repoPath := filepath.Join(workdir, "source")
	newSHA, err := seedCommit(repoPath, fixtureRemote)
	if err != nil {
		return fmt.Errorf("seed source commit: %w", err)
	}

	branch := fmt.Sprintf("foundry/e2e/%d", time.Now().UTC().Unix())
	fmt.Printf("pushing branch %s (%s) via kernel.PushBranch...\n", branch, newSHA)

	leases := kernel.NewPGLeaseStore(db)
	ledger := extops.NewStore(db)

	receipt, err := kernel.PushBranch(ctx, leases, ledger, write.EnvTokenSource{}, write.PushRequest{
		RepoPath:     repoPath,
		Branch:       branch,
		ExpectedBase: "", // fresh branch: must not already exist on the fixture remote
		NewSHA:       newSHA,
		WorkflowID:   "e2e-github",
	})
	if err != nil {
		return fmt.Errorf("kernel.PushBranch: %w", err)
	}
	fmt.Printf("push receipt: before=%s after=%s url=%s\n", receipt.BeforeSHA, receipt.AfterSHA, receipt.URL)

	got, err := read.ResolveRef(ctx, fixtureRemote, branch)
	if err != nil {
		return fmt.Errorf("verify: resolve %s on fixture remote: %w", branch, err)
	}
	if got != newSHA {
		return fmt.Errorf("verify: fixture remote's %s = %s, want %s", branch, got, newSHA)
	}
	fmt.Printf("verified: fixture remote's %s now points at %s\n", branch, got)

	var state string
	target := fixtureRemote + "#" + branch
	if err := db.QueryRowContext(ctx,
		`SELECT state FROM external_operations WHERE target = $1 AND kind = 'scm.push' ORDER BY created_at DESC LIMIT 1`,
		target,
	).Scan(&state); err != nil {
		return fmt.Errorf("verify: query ledger for target %s: %w", target, err)
	}
	if state != string(extops.StateExecuted) {
		return fmt.Errorf("verify: ledger state for %s = %q, want %q", target, state, extops.StateExecuted)
	}
	fmt.Printf("verified: extops ledger recorded state=%s for %s\n", state, target)

	fmt.Println("e2e-github: PASS")
	return nil
}

// seedCommit creates a fresh source repo at repoPath with "origin"
// pointing at remoteURL and one commit, returning that commit's SHA. It
// does not push anything — kernel.PushBranch performs the only push.
func seedCommit(repoPath, remoteURL string) (string, error) {
	repo, err := git.PlainInit(repoPath, false)
	if err != nil {
		return "", fmt.Errorf("init source repo: %w", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteURL}}); err != nil {
		return "", fmt.Errorf("create origin remote: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "DELIVERY.md"), []byte("foundry e2e-github fixture delivery\n"), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	if _, err := wt.Add("DELIVERY.md"); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	sig := &object.Signature{Name: "foundry-e2e-github", Email: "noreply@example.invalid", When: time.Now()}
	h, err := wt.Commit("e2e-github fixture delivery", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return h.String(), nil
}

// ensureSchema creates the leases and external_operations tables if they
// do not already exist, so this program does not depend on `make
// migrate-up` having already run — the same defensive pattern
// internal/ledger/extops's own tests use.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	const leasesDDL = `
CREATE TABLE IF NOT EXISTS leases (
    resource   TEXT PRIMARY KEY,
    token      TEXT NOT NULL,
    holder     TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
)`
	if _, err := db.ExecContext(ctx, leasesDDL); err != nil {
		return fmt.Errorf("ensure leases table exists: %w", err)
	}

	const extopsDDL = `
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
	if _, err := db.ExecContext(ctx, extopsDDL); err != nil {
		return fmt.Errorf("ensure external_operations table exists: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
