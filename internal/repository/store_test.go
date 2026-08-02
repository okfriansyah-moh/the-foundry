package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/repository"
)

func TestNormalizeAndStore(t *testing.T) {
	s := repository.NewMemStore()
	rec := repository.Record{
		ID: "repo-1", Provider: repository.ProviderGitHub,
		CanonicalURL:       "https://GitHub.com/Example/X.git/",
		ProfileID:          "personal-autonomous-venture",
		PinnedBaseRevision: "abc123",
	}
	if err := s.Upsert(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "repo-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalURL != "https://github.com/Example/X" {
		t.Fatalf("canonical = %q", got.CanonicalURL)
	}
	byURL, err := s.GetByCanonicalURL(context.Background(), "https://github.com/Example/X")
	if err != nil || byURL.ID != "repo-1" {
		t.Fatalf("by url: %+v %v", byURL, err)
	}
}

func TestResolverOwnershipAndPin(t *testing.T) {
	s := repository.NewMemStore()
	_ = s.Upsert(context.Background(), repository.Record{
		ID: "repo-2", Provider: repository.ProviderBitbucket,
		CanonicalURL: "https://bitbucket.org/org/x",
		ProfileID:    "organization-10x", OrganizationID: "org-1",
	})
	r := repository.Resolver{Store: s}
	_, err := r.Resolve(context.Background(), repository.ResolveInput{
		RepositoryID: "repo-2", ProfileID: "personal-autonomous-venture", RequirePinned: true,
	})
	if !errors.Is(err, repository.ErrWrongOwner) {
		t.Fatalf("want wrong owner, got %v", err)
	}
	_, err = r.Resolve(context.Background(), repository.ResolveInput{
		RepositoryID: "repo-2", ProfileID: "organization-10x", OrganizationID: "org-1", RequirePinned: true,
	})
	if !errors.Is(err, repository.ErrStaleRevision) {
		t.Fatalf("want stale revision, got %v", err)
	}
}

func TestResolverLocalPathTraversal(t *testing.T) {
	s := repository.NewMemStore()
	_ = s.Upsert(context.Background(), repository.Record{
		ID: "repo-local", Provider: repository.ProviderLocal,
		CanonicalURL: "file:///tmp/../etc/passwd",
		ProfileID:    "personal-autonomous-venture", PinnedBaseRevision: "local",
	})
	r := repository.Resolver{Store: s}
	_, err := r.Resolve(context.Background(), repository.ResolveInput{
		RepositoryID: "repo-local", ProfileID: "personal-autonomous-venture",
		AllowedLocalRoots: []string{"/var/foundry/work"},
	})
	if !errors.Is(err, repository.ErrPathRefused) {
		t.Fatalf("want path refused, got %v", err)
	}
}
