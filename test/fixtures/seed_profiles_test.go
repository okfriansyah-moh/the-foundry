package fixtures

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/identity"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

func TestSeedProfilesIsIdempotent(t *testing.T) {
	ctx := context.Background()
	idStore := identity.NewMemStore()
	profileStore := profile.NewStore(profile.NewMemRawStore())

	if err := SeedProfiles(ctx, idStore, profileStore); err != nil {
		t.Fatalf("first SeedProfiles: %v", err)
	}
	if err := SeedProfiles(ctx, idStore, profileStore); err != nil {
		t.Fatalf("second SeedProfiles (must be a no-op): %v", err)
	}

	orgs, err := idStore.ListOrganizations(ctx)
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("ListOrganizations = %d orgs, want exactly 1 (no duplicate from second seed)", len(orgs))
	}

	principals, err := idStore.ListPrincipals(ctx)
	if err != nil {
		t.Fatalf("ListPrincipals: %v", err)
	}
	if len(principals) != 1 {
		t.Fatalf("ListPrincipals = %d principals, want exactly 1", len(principals))
	}

	members, err := idStore.ListOrgMembers(ctx, devOrgID)
	if err != nil {
		t.Fatalf("ListOrgMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("ListOrgMembers = %d members, want exactly 1", len(members))
	}

	profiles, err := profileStore.List(ctx)
	if err != nil {
		t.Fatalf("List profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("List profiles = %d, want exactly 2 (dev-personal, dev-org)", len(profiles))
	}

	personal, err := profileStore.Load(ctx, DevPersonalProfileID)
	if err != nil {
		t.Fatalf("Load %s: %v", DevPersonalProfileID, err)
	}
	if personal.Kind != profile.Personal {
		t.Fatalf("%s kind = %s, want personal", DevPersonalProfileID, personal.Kind)
	}

	org, err := profileStore.Load(ctx, DevOrgProfileID)
	if err != nil {
		t.Fatalf("Load %s: %v", DevOrgProfileID, err)
	}
	if org.Kind != profile.Organization {
		t.Fatalf("%s kind = %s, want organization", DevOrgProfileID, org.Kind)
	}
	if org.OrgID == nil || *org.OrgID != devOrgID {
		t.Fatalf("%s OrgID = %v, want %s", DevOrgProfileID, org.OrgID, devOrgID)
	}
}
