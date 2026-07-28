package write

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// BitbucketPusher emulates compare-and-swap semantics for Bitbucket Cloud via
// fetch/verify/push/post-verify under the same lease discipline the GitHub
// adapter uses.
type BitbucketPusher struct {
	Leases LeaseAcquirer
	Ledger Ledger
	Tokens TokenSource
	Holder string
}

func (p *BitbucketPusher) PushBranch(ctx context.Context, req PushRequest) (Receipt, error) {
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
	defer func() { _ = p.Leases.Release(ctx, resource, token) }()

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

	var authToken string
	if isHTTPRemote(remoteIdentity) {
		authToken, err = p.Tokens.Token(ctx)
		if err != nil {
			return Receipt{}, err
		}
	}
	beforeSHA, err := bitbucketRemoteHead(ctx, remote, req.Branch, authToken)
	if err != nil {
		return Receipt{}, err
	}
	if beforeSHA != req.ExpectedBase {
		return Receipt{}, fmt.Errorf("scm/write: bitbucket verify branch %s: expected %q, got %q", req.Branch, req.ExpectedBase, beforeSHA)
	}
	if err := repo.FetchContext(ctx, &git.FetchOptions{RemoteName: remote.Config().Name}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return Receipt{}, fmt.Errorf("scm/write: bitbucket fetch before push: %w", err)
	}
	if _, err := push(ctx, remote, req, authToken); err != nil {
		return Receipt{}, fmt.Errorf("scm/write: push %s to %s: %w", req.Branch, remoteIdentity, err)
	}
	afterSHA, err := bitbucketRemoteHead(ctx, remote, req.Branch, authToken)
	if err != nil {
		return Receipt{}, err
	}
	if afterSHA != req.NewSHA {
		return Receipt{}, fmt.Errorf("scm/write: bitbucket post-push verify branch %s: expected %q, got %q", req.Branch, req.NewSHA, afterSHA)
	}
	receipt := Receipt{BeforeSHA: beforeSHA, AfterSHA: afterSHA, URL: bitbucketBranchURL(remoteIdentity, req.Branch)}
	if _, err := p.Ledger.MarkExecuted(ctx, op.ID, receipt); err != nil {
		return Receipt{}, fmt.Errorf("scm/write: record receipt for %s: %w", target, err)
	}
	return receipt, nil
}

func bitbucketRemoteHead(ctx context.Context, remote *git.Remote, branch, token string) (string, error) {
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: authFor(remoteURL(remote), token)})
	if err != nil {
		return "", fmt.Errorf("scm/write: list remote refs for %s: %w", branch, err)
	}
	refName := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == refName {
			return ref.Hash().String(), nil
		}
	}
	return "", nil
}

func bitbucketBranchURL(remoteIdentity, branch string) string {
	https := strings.TrimSuffix(strings.TrimPrefix(remoteIdentity, "https://bitbucket.org/"), ".git")
	if https != remoteIdentity {
		return "https://bitbucket.org/" + https + "/src/" + branch
	}
	ssh := strings.TrimSuffix(strings.TrimPrefix(remoteIdentity, "git@bitbucket.org:"), ".git")
	if ssh != remoteIdentity {
		return "https://bitbucket.org/" + ssh + "/src/" + branch
	}
	return remoteIdentity + "@" + branch
}

var _ = time.Minute
var _ = config.RefSpec("")
