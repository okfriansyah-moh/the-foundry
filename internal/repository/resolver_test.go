package repository_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/repository"
)

func TestResolverHappyPath(t *testing.T) {
	s := repository.NewMemStore()
	if err := s.Upsert(context.Background(), repository.Record{
		ID: "repo-ok", Provider: repository.ProviderGitHub,
		CanonicalURL:        "https://github.com/acme/app",
		ProfileID:           "personal-autonomous-venture",
		PinnedBaseRevision:  "deadbeef",
		DefaultTargetBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := (repository.Resolver{Store: s}).Resolve(context.Background(), repository.ResolveInput{
		RepositoryID: "repo-ok", ProfileID: "personal-autonomous-venture", RequirePinned: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.PinnedBaseRevision != "deadbeef" {
		t.Fatalf("pin = %q", out.Record.PinnedBaseRevision)
	}
}
