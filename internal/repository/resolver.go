package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveInput is the kernel-facing intent for repository resolution.
type ResolveInput struct {
	RepositoryID   string
	ProfileID      string
	OrganizationID string
	// AllowedLocalRoots, when set, confines ProviderLocal paths.
	AllowedLocalRoots []string
	// RequirePinned refuses records without a pinned base revision.
	RequirePinned bool
}

// ResolveOutput is the immutable repository context for an envelope.
type ResolveOutput struct {
	Record Record
}

// Resolver loads and validates owned repository records for the kernel.
type Resolver struct {
	Store Store
}

// Resolve looks up a repository by ID and enforces ownership + revision rules.
func (r Resolver) Resolve(ctx context.Context, in ResolveInput) (ResolveOutput, error) {
	if r.Store == nil {
		return ResolveOutput{}, fmt.Errorf("%w: missing repository store", ErrInvalid)
	}
	if in.RepositoryID == "" {
		return ResolveOutput{}, fmt.Errorf("%w: missing repository id", ErrInvalid)
	}
	rec, err := r.Store.Get(ctx, in.RepositoryID)
	if err != nil {
		return ResolveOutput{}, err
	}
	if in.ProfileID != "" && rec.ProfileID != "" && in.ProfileID != rec.ProfileID {
		return ResolveOutput{}, fmt.Errorf("%w: profile %q cannot use repo owned by %q", ErrWrongOwner, in.ProfileID, rec.ProfileID)
	}
	if in.OrganizationID != "" && rec.OrganizationID != "" && in.OrganizationID != rec.OrganizationID {
		return ResolveOutput{}, fmt.Errorf("%w: organization mismatch", ErrWrongOwner)
	}
	if in.RequirePinned && rec.PinnedBaseRevision == "" {
		return ResolveOutput{}, fmt.Errorf("%w: repository %s", ErrStaleRevision, rec.ID)
	}
	if rec.Provider == ProviderLocal {
		if err := refuseUnsafeLocalPath(rec.CanonicalURL, in.AllowedLocalRoots); err != nil {
			return ResolveOutput{}, err
		}
	}
	return ResolveOutput{Record: rec}, nil
}

func refuseUnsafeLocalPath(canonical string, roots []string) error {
	path := strings.TrimPrefix(canonical, "file://")
	if path == "" {
		return fmt.Errorf("%w: empty local path", ErrPathRefused)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("%w: path traversal", ErrPathRefused)
	}
	if len(roots) == 0 {
		// No configured roots: refuse absolute production local paths that look
		// like canonical clones outside a worktree root.
		if strings.Contains(path, "/.git") || strings.HasSuffix(path, ".git") {
			return fmt.Errorf("%w: canonical clone target", ErrPathRefused)
		}
		return nil
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s outside allowed roots", ErrPathRefused, clean)
}
