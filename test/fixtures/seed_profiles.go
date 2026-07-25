// Package fixtures also seeds the two profiles docs/PLAN.md Task 21
// (FND-02) names as shared e2e fixtures: "dev-personal" (kind personal) and
// "dev-org" (kind organization, backed by a "dev-org" organization and
// principal). SeedProfiles is idempotent — running it twice against the
// same stores neither errors nor duplicates rows — so every later e2e suite
// can call it unconditionally as setup.
package fixtures

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/identity"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

// DevPersonalProfileID and DevOrgProfileID are the fixed IDs every later
// e2e suite can depend on.
const (
	DevPersonalProfileID = "dev-personal"
	DevOrgProfileID      = "dev-org"
	devOrgID             = "dev-org"
	devOrgOwnerID        = "dev-org-owner"
)

// devConfig is a minimal config satisfying config/schemas/profile.schema.json
// (schema_version 1, a budget.max_usd).
func devConfig(maxUSD float64) json.RawMessage {
	data, err := json.Marshal(map[string]interface{}{
		"schema_version": profile.ConfigSchemaVersion,
		"budget":         map[string]interface{}{"max_usd": maxUSD},
	})
	if err != nil {
		// Marshaling a literal map[string]interface{} of scalars never
		// fails; a panic here means the literal above was edited to
		// contain something unmarshalable.
		panic(fmt.Sprintf("fixtures: build dev config: %v", err))
	}
	return data
}

// policyDigestFor mirrors cmd/foundry/profile.go's placeholder digest
// (Task 22's policy compiler does not exist yet) so seeded fixtures use the
// same convention real `foundry profile create` calls would.
func policyDigestFor(config json.RawMessage) string {
	return fmt.Sprintf("sha256-placeholder:%d", len(config))
}

// SeedProfiles idempotently creates the dev-org organization, its owner
// principal, and the two shared e2e profiles (DevPersonalProfileID,
// DevOrgProfileID) in idStore/profileStore. A second call against the same
// stores is a no-op: rows that already exist (identity.ErrAlreadyExists /
// profile.ErrAlreadyExists) are treated as already-seeded, not an error.
func SeedProfiles(ctx context.Context, idStore identity.Store, profileStore *profile.Store) error {
	org := &identity.Organization{ID: devOrgID, Name: "Dev Org"}
	if err := idStore.CreateOrganization(ctx, org); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		return fmt.Errorf("fixtures: seed organization %s: %w", devOrgID, err)
	}

	owner := &identity.Principal{ID: devOrgOwnerID, Kind: identity.PrincipalHuman, Display: "Dev Org Owner"}
	if err := idStore.CreatePrincipal(ctx, owner); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		return fmt.Errorf("fixtures: seed principal %s: %w", devOrgOwnerID, err)
	}

	if err := idStore.AddOrgMember(ctx, &identity.OrgMember{OrgID: devOrgID, PrincipalID: devOrgOwnerID, Role: "owner"}); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		return fmt.Errorf("fixtures: seed org member %s/%s: %w", devOrgID, devOrgOwnerID, err)
	}

	personalConfig := devConfig(100)
	personal := &profile.Profile{
		ID:           DevPersonalProfileID,
		Name:         "Dev Personal",
		Kind:         profile.Personal,
		Config:       personalConfig,
		PolicyDigest: policyDigestFor(personalConfig),
	}
	if err := profileStore.Save(ctx, personal); err != nil && !errors.Is(err, profile.ErrAlreadyExists) {
		return fmt.Errorf("fixtures: seed profile %s: %w", DevPersonalProfileID, err)
	}

	orgConfig := devConfig(1000)
	orgProfile := &profile.Profile{
		ID:           DevOrgProfileID,
		Name:         "Dev Org",
		Kind:         profile.Organization,
		OrgID:        strPtr(devOrgID),
		Config:       orgConfig,
		PolicyDigest: policyDigestFor(orgConfig),
	}
	if err := profileStore.Save(ctx, orgProfile); err != nil && !errors.Is(err, profile.ErrAlreadyExists) {
		return fmt.Errorf("fixtures: seed profile %s: %w", DevOrgProfileID, err)
	}

	return nil
}

func strPtr(s string) *string { return &s }
