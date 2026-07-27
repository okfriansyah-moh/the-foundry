package read

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Mirror creates a bare mirror clone of repoURL at mirrorPath if nothing
// exists there yet, or brings an existing mirror up to date in place
// (equivalent to `git clone --mirror` followed by `git remote update`) if
// it does. Constitution C4 note: this only ever reads from repoURL and
// writes to the caller's own local mirrorPath — it never mutates
// repoURL's remote state, so it carries none of internal/scm/write's
// authority restriction.
func Mirror(ctx context.Context, repoURL, mirrorPath string) error {
	if repoURL == "" {
		return fmt.Errorf("scm/read: repoURL is empty")
	}
	if mirrorPath == "" {
		return fmt.Errorf("scm/read: mirrorPath is empty")
	}

	_, err := git.PlainOpen(mirrorPath)
	switch {
	case err == nil:
		return Fetch(ctx, mirrorPath)
	case errors.Is(err, git.ErrRepositoryNotExists):
		if _, cloneErr := git.PlainCloneContext(ctx, mirrorPath, true, &git.CloneOptions{
			URL:    repoURL,
			Mirror: true,
		}); cloneErr != nil {
			return fmt.Errorf("scm/read: mirror clone %s into %s: %w", repoURL, mirrorPath, cloneErr)
		}
		return nil
	default:
		return fmt.Errorf("scm/read: open %s: %w", mirrorPath, err)
	}
}

// Fetch updates an existing mirror at mirrorPath from its configured
// "origin" remote in place. It is a no-op error (nil) if the mirror was
// already up to date.
func Fetch(ctx context.Context, mirrorPath string) error {
	repo, err := git.PlainOpen(mirrorPath)
	if err != nil {
		return fmt.Errorf("scm/read: open %s: %w", mirrorPath, err)
	}

	// No FetchOptions.Force here: Mirror's initial PlainCloneContext(...,
	// Mirror: true) already configures "origin"'s fetch refspec with the
	// "+" (non-fast-forward-allowed) prefix go-git's Mirror option sets up
	// for exactly this reason, so a plain fetch already updates every ref
	// to match origin, including non-fast-forward moves upstream.
	err = repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin"})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("scm/read: fetch %s: %w", mirrorPath, err)
	}
	return nil
}

// ResolveRef resolves ref (a branch name, tag, "HEAD", or a commit SHA) to
// its current commit SHA within the repository at repoPath. repoPath may
// be a mirror, a worktree, or any other local git repository — ResolveRef
// only ever reads.
func ResolveRef(ctx context.Context, repoPath, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("scm/read: ref is empty")
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("scm/read: open %s: %w", repoPath, err)
	}

	h, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("scm/read: resolve %q in %s: %w", ref, repoPath, err)
	}
	return h.String(), nil
}
