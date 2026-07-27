package write

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// pushLeaseTTL bounds how long PushBranch holds its "repo:branch" fencing
// lease. A push is a single short round trip, so this is generous rather
// than tight — the lease is released explicitly on every return path
// regardless.
const pushLeaseTTL = 2 * time.Minute

// defaultHolder is the lease holder identity used when Pusher.Holder is
// left empty.
const defaultHolder = "scm-write"

// defaultRemoteName is the git remote name used when PushRequest.
// RemoteName is left empty.
const defaultRemoteName = "origin"

// branchPattern is deliberately conservative: letters/digits/underscore/
// dot/dash/slash only, must start with an alphanumeric, no "..", and never
// a leading/trailing/doubled slash — enough to rule out anything that
// could be misread as refspec syntax (":", "+", "*", whitespace) once
// embedded in "<sha>:refs/heads/<branch>".
var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,250}$`)

// shaPattern requires a full, unabbreviated 40-character hex SHA-1 — the
// unambiguous form for both the refspec source and RequireRemoteRefs.
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// LeaseAcquirer is the minimal lease behavior PushBranch depends on,
// defined here in the consuming package rather than imported from
// internal/kernel: this package must never import internal/kernel (kernel
// is this package's only permitted importer — Constitution C4 — so the
// reverse import would be a cycle). internal/kernel's lease stores satisfy
// this interface structurally via a small adapter kernel defines for
// itself (internal/kernel/scmpush.go); nothing here needs to know that.
type LeaseAcquirer interface {
	// Acquire grants (or idempotently re-grants, for the same holder) a
	// fencing token for resource.
	Acquire(ctx context.Context, resource, holder string, ttl time.Duration) (token string, err error)
	// Release gives up resource if token is still its current holder's
	// token. Releasing a resource/token pair that is not currently held
	// (already expired, already released, or held by someone else) is a
	// safe no-op, not an error.
	Release(ctx context.Context, resource, token string) error
}

// Ledger is the subset of internal/ledger/extops.Store's behavior
// PushBranch needs to record a push receipt exactly once per idempotency
// key (Constitution C9; docs/PLAN.md Task 26). Defined locally (same
// pattern as internal/kernel.ExternalOpStore) so this package depends on
// the shape it needs, not the concrete store.
type Ledger interface {
	Reserve(ctx context.Context, workflowID, kind, target, idempotencyKey string, request any) (extops.Op, error)
	MarkExecuted(ctx context.Context, id extops.OpID, receipt any) (extops.Op, error)
}

// Receipt is the durable proof of one push, recorded to the extops ledger
// and returned to the caller.
type Receipt struct {
	BeforeSHA string `json:"before_sha"`
	AfterSHA  string `json:"after_sha"`
	URL       string `json:"url"`
}

// PushRequest describes one branch push.
type PushRequest struct {
	// RepoPath is the local git repository (a worktree or mirror) that
	// already contains NewSHA in its object store.
	RepoPath string
	// RemoteName is the git remote to push to. Empty means "origin".
	RemoteName string
	// RemoteURL is used to create RemoteName in RepoPath if it is not
	// already configured there. Ignored if RemoteName already exists.
	RemoteURL string
	// Branch is the destination branch name, without a "refs/heads/"
	// prefix (e.g. "foundry/e2e/172...", not "refs/heads/foundry/...").
	Branch string
	// ExpectedBase is the full 40-character SHA the caller believes
	// Branch currently points to on the remote, or "" to mean "Branch
	// must not exist yet on the remote".
	ExpectedBase string
	// NewSHA is the full 40-character SHA to push as Branch's new tip.
	// It must already exist as a commit object in RepoPath.
	NewSHA string
	// WorkflowID scopes the ledger row this push is recorded under.
	WorkflowID string
	// IdempotencyKey lets a caller supply its own ledger key (e.g. so a
	// Temporal activity retry replays instead of re-pushing). Empty
	// derives a deterministic key from RepoPath/Branch/NewSHA.
	IdempotencyKey string
}

func (r PushRequest) idempotencyKey() string {
	if r.IdempotencyKey != "" {
		return r.IdempotencyKey
	}
	return fmt.Sprintf("scm.push:%s:%s:%s", r.RepoPath, r.Branch, r.NewSHA)
}

func (r PushRequest) validate() error {
	if r.RepoPath == "" {
		return errors.New("scm/write: RepoPath is empty")
	}
	if !branchPattern.MatchString(r.Branch) || strings.Contains(r.Branch, "..") || strings.Contains(r.Branch, "//") {
		return fmt.Errorf("scm/write: invalid Branch %q", r.Branch)
	}
	if !shaPattern.MatchString(r.NewSHA) {
		return fmt.Errorf("scm/write: NewSHA must be a full 40-character hex SHA, got %q", r.NewSHA)
	}
	if r.ExpectedBase != "" && !shaPattern.MatchString(r.ExpectedBase) {
		return fmt.Errorf("scm/write: ExpectedBase must be a full 40-character hex SHA or empty, got %q", r.ExpectedBase)
	}
	return nil
}

// Pusher performs the Task 27 push protocol: acquire a lease on
// "repo:branch" -> compare-and-swap push (server-side, never force) ->
// record the receipt to the extops ledger -> release the lease.
type Pusher struct {
	Leases LeaseAcquirer
	Ledger Ledger
	Tokens TokenSource
	// Holder is this Pusher's lease holder identity. Empty means
	// defaultHolder.
	Holder string
}

// PushBranch pushes req.NewSHA onto req.Branch in req.RepoPath's
// req.RemoteName remote, but only if the remote's current tip is exactly
// req.ExpectedBase (or, if req.ExpectedBase is "", only if req.Branch does
// not exist there yet). This is enforced server-side via
// git.PushOptions.RequireRemoteRefs plus a non-force update refspec — see
// doc.go for why that is the real, not merely client-side, CAS mechanism.
//
// On success, a receipt is recorded to the ledger exactly once per
// req.IdempotencyKey (Constitution C9): a second call with the same key
// returns the previously recorded receipt without pushing again, so this
// is safe to retry after a crash between the push landing and the caller
// observing success.
func (p *Pusher) PushBranch(ctx context.Context, req PushRequest) (Receipt, error) {
	if err := req.validate(); err != nil {
		return Receipt{}, err
	}

	holder := p.Holder
	if holder == "" {
		holder = defaultHolder
	}

	repo, err := git.PlainOpen(req.RepoPath)
	if err != nil {
		return Receipt{}, fmt.Errorf("scm/write: open %s: %w", req.RepoPath, err)
	}
	remote, err := resolveRemote(repo, req.RemoteName, req.RemoteURL)
	if err != nil {
		return Receipt{}, err
	}
	remoteIdentity := remoteURL(remote)
	resource := fmt.Sprintf("scm-push:%s:%s", remoteIdentity, req.Branch)
	target := fmt.Sprintf("%s#%s", remoteIdentity, req.Branch)

	token, err := p.Leases.Acquire(ctx, resource, holder, pushLeaseTTL)
	if err != nil {
		return Receipt{}, fmt.Errorf("scm/write: acquire lease %s: %w", resource, err)
	}
	defer func() {
		_ = p.Leases.Release(ctx, resource, token)
	}()

	op, err := p.Ledger.Reserve(ctx, req.WorkflowID, "scm.push", target, req.idempotencyKey(), req)
	if err != nil {
		return Receipt{}, fmt.Errorf("scm/write: reserve ledger op for %s: %w", target, err)
	}
	if op.State == extops.StateExecuted || op.State == extops.StateReconciled {
		var receipt Receipt
		if err := json.Unmarshal(op.Receipt, &receipt); err != nil {
			return Receipt{}, fmt.Errorf("scm/write: decode existing receipt for %s: %w", req.idempotencyKey(), err)
		}
		return receipt, nil
	}

	// A token is only needed (and only fetched) for http(s) remotes —
	// e.g. this package's own file://-transport fixture tests need none,
	// and must not be made to depend on TokenSource being configured.
	var ghToken string
	if isHTTPRemote(remoteIdentity) {
		ghToken, err = p.Tokens.Token(ctx)
		if err != nil {
			return Receipt{}, err
		}
	}

	receipt, err := push(ctx, remote, req, ghToken)
	if err != nil {
		// The push did not land: leave the ledger op reserved so a retry
		// with the same idempotency key tries again, rather than
		// recording a receipt for a push that never happened.
		return Receipt{}, fmt.Errorf("scm/write: push %s to %s: %w", req.Branch, remoteIdentity, err)
	}
	receipt.URL = branchURL(remoteIdentity, req.Branch)

	if _, err := p.Ledger.MarkExecuted(ctx, op.ID, receipt); err != nil {
		return Receipt{}, fmt.Errorf("scm/write: record receipt for %s: %w", target, err)
	}
	return receipt, nil
}

// push performs the actual git push. Force is intentionally never set on
// the returned git.PushOptions — see doc.go's Boundary paragraph. CAS is
// instead enforced via RequireRemoteRefs (when ExpectedBase is non-empty)
// plus a non-force refspec, which go-git checks against a fresh
// advertisement it fetches from the remote at the start of this exact
// call (github.com/go-git/go-git/v5's Remote.PushContext), and which the
// remote's own receive-pack re-validates atomically when applying the
// update — a real round trip, not a comparison against data read earlier.
func push(ctx context.Context, remote *git.Remote, req PushRequest, token string) (Receipt, error) {
	refSpec := config.RefSpec(fmt.Sprintf("%s:refs/heads/%s", req.NewSHA, req.Branch))
	if err := refSpec.Validate(); err != nil {
		return Receipt{}, fmt.Errorf("invalid refspec: %w", err)
	}

	opts := &git.PushOptions{
		RemoteName: remote.Config().Name,
		RefSpecs:   []config.RefSpec{refSpec},
	}
	if req.ExpectedBase != "" {
		requireSpec := config.RefSpec(fmt.Sprintf("%s:refs/heads/%s", req.ExpectedBase, req.Branch))
		if err := requireSpec.Validate(); err != nil {
			return Receipt{}, fmt.Errorf("invalid require-remote-ref spec: %w", err)
		}
		opts.RequireRemoteRefs = []config.RefSpec{requireSpec}
	}
	if auth := authFor(remoteURL(remote), token); auth != nil {
		opts.Auth = auth
	}

	if err := remote.PushContext(ctx, opts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return Receipt{}, err
	}

	return Receipt{BeforeSHA: req.ExpectedBase, AfterSHA: req.NewSHA}, nil
}

// authFor returns HTTP basic auth for http(s) remotes when a token is
// available, and nil otherwise (e.g. the file:// transport this package's
// own fixture tests use, which ignores Auth entirely).
func authFor(remote, token string) *http.BasicAuth {
	if token == "" || !isHTTPRemote(remote) {
		return nil
	}
	// GitHub accepts any non-empty username with a PAT as the password;
	// "x-access-token" matches GitHub's own documented convention.
	return &http.BasicAuth{Username: "x-access-token", Password: token}
}

// isHTTPRemote reports whether remote is an http(s) URL — the only
// transport this package ever needs a token for (e.g. the file://
// transport this package's own fixture tests use needs none).
func isHTTPRemote(remote string) bool {
	return strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://")
}

// resolveRemote returns repo's remote named name, creating it from url if
// it does not already exist.
func resolveRemote(repo *git.Repository, name, url string) (*git.Remote, error) {
	if name == "" {
		name = defaultRemoteName
	}
	remote, err := repo.Remote(name)
	if err == nil {
		return remote, nil
	}
	if !errors.Is(err, git.ErrRemoteNotFound) {
		return nil, fmt.Errorf("scm/write: look up remote %q: %w", name, err)
	}
	if url == "" {
		return nil, fmt.Errorf("scm/write: remote %q is not configured and no RemoteURL was given", name)
	}
	remote, err = repo.CreateRemote(&config.RemoteConfig{Name: name, URLs: []string{url}})
	if err != nil {
		return nil, fmt.Errorf("scm/write: create remote %q: %w", name, err)
	}
	return remote, nil
}

func remoteURL(remote *git.Remote) string {
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

// githubHTTPSPattern extracts owner/repo from an https GitHub remote URL.
var githubHTTPSPattern = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(\.git)?/?$`)

// githubSSHPattern extracts owner/repo from an ssh GitHub remote URL.
var githubSSHPattern = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(\.git)?/?$`)

// branchURL builds a human-followable URL for the pushed branch when
// remoteIdentity is recognizably a GitHub remote, falling back to a plain
// "<remote>@<branch>" identifier otherwise (e.g. this package's own
// file://-transport fixture tests).
func branchURL(remoteIdentity, branch string) string {
	for _, re := range []*regexp.Regexp{githubHTTPSPattern, githubSSHPattern} {
		if m := re.FindStringSubmatch(remoteIdentity); m != nil {
			return fmt.Sprintf("https://github.com/%s/%s/tree/%s", m[1], m[2], branch)
		}
	}
	return remoteIdentity + "@" + branch
}
