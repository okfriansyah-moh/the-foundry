package write

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// BitbucketPusher performs the same Task 27 push protocol as Pusher
// (lease → server-side CAS via shared push() → extops receipt → release),
// authenticated for Bitbucket Cloud. Authentication username is owned by
// this adapter (ProviderBitbucket → "x-token-auth"), never sniffed from
// the remote URL (docs/PLAN.md Task 137).
//
// decision (Task 137 step 7): the previous client-side pre-push head check
// and post-push re-list were removed. Shared push()'s RequireRemoteRefs +
// non-force refspec already enforces CAS against a fresh remote
// advertisement at push time; the extra list round-trips duplicated that
// check client-side without adding a tested guarantee the server-side CAS
// lacked, and they used the same authFor path that previously hard-coded
// GitHub's username. Aligning with Pusher keeps one CAS mechanism.
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
	receipt, err := push(ctx, remote, req, ProviderBitbucket, authToken)
	if err != nil {
		return Receipt{}, fmt.Errorf("scm/write: push %s to %s: %w", req.Branch, remoteIdentity, err)
	}
	receipt.URL = bitbucketBranchURL(remoteIdentity, req.Branch)
	if _, err := p.Ledger.MarkExecuted(ctx, op.ID, receipt); err != nil {
		return Receipt{}, fmt.Errorf("scm/write: record receipt for %s: %w", target, err)
	}
	return receipt, nil
}

// bitbucketHTTPSPattern extracts workspace/repo from an https Bitbucket remote.
var bitbucketHTTPSPattern = regexp.MustCompile(`^https://bitbucket\.org/([^/]+)/([^/]+?)(\.git)?/?$`)

// bitbucketSSHPattern extracts workspace/repo from an ssh Bitbucket remote.
var bitbucketSSHPattern = regexp.MustCompile(`^git@bitbucket\.org:([^/]+)/([^/]+?)(\.git)?/?$`)

func bitbucketBranchURL(remoteIdentity, branch string) string {
	for _, re := range []*regexp.Regexp{bitbucketHTTPSPattern, bitbucketSSHPattern} {
		if m := re.FindStringSubmatch(remoteIdentity); m != nil {
			return fmt.Sprintf("https://bitbucket.org/%s/%s/src/%s", m[1], m[2], branch)
		}
	}
	if strings.Contains(remoteIdentity, "bitbucket.org") {
		https := strings.TrimSuffix(strings.TrimPrefix(remoteIdentity, "https://bitbucket.org/"), ".git")
		if https != remoteIdentity {
			return "https://bitbucket.org/" + https + "/src/" + branch
		}
	}
	return remoteIdentity + "@" + branch
}
