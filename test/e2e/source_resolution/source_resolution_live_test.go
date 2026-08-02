package sourcesresolution_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/repository"
)

func TestSourceResolution_RegistryToPinnedRevision(t *testing.T) {
	store := repository.NewMemStore()
	if err := store.Upsert(context.Background(), repository.Record{
		ID: "repo-e2e", Provider: repository.ProviderGitHub,
		CanonicalURL:       "https://github.com/acme/demo",
		ProfileID:          "personal-autonomous-venture",
		PinnedBaseRevision: "cafe1234",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := (repository.Resolver{Store: store}).Resolve(context.Background(), repository.ResolveInput{
		RepositoryID: "repo-e2e", ProfileID: "personal-autonomous-venture", RequirePinned: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.PinnedBaseRevision != "cafe1234" {
		t.Fatalf("pin = %q", out.Record.PinnedBaseRevision)
	}
}
