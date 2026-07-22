package profile

import (
	"encoding/json"
	"fmt"
	"time"
)

// Kind enumerates the values internal/db/migrations/00005_profiles.sql
// constrains profiles.kind to (CHECK (kind IN ('personal', 'organization'))).
type Kind string

const (
	Personal     Kind = "personal"
	Organization Kind = "organization"
)

// Valid reports whether k is one of the kinds the profiles table accepts.
func (k Kind) Valid() bool {
	switch k {
	case Personal, Organization:
		return true
	default:
		return false
	}
}

// Profile is the typed view of a profiles row.
type Profile struct {
	ID           string
	Name         string
	Kind         Kind
	OrgID        *string // nullable org_id column; must be set iff Kind == Organization
	Config       json.RawMessage
	PolicyDigest string
	CreatedAt    time.Time
}

// Validate checks p against the constraints the DB schema enforces plus the
// profile.schema.json config contract, so callers fail fast with a
// field-scoped error (including a JSON-pointer path for config violations)
// instead of a bare constraint violation from Postgres.
func (p *Profile) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("profile: id required")
	}
	if p.Name == "" {
		return fmt.Errorf("profile: name required")
	}
	if !p.Kind.Valid() {
		return fmt.Errorf("profile: kind %q invalid (want personal|organization)", p.Kind)
	}
	if p.Kind == Organization && (p.OrgID == nil || *p.OrgID == "") {
		return fmt.Errorf("profile: org_id required when kind=organization")
	}
	if p.Kind == Personal && p.OrgID != nil {
		return fmt.Errorf("profile: org_id must be unset when kind=personal")
	}
	if p.PolicyDigest == "" {
		return fmt.Errorf("profile: policy_digest required")
	}
	if len(p.Config) == 0 {
		return fmt.Errorf("profile: config required")
	}
	if err := ValidateConfig(p.Config); err != nil {
		return fmt.Errorf("profile: config invalid: %w", err)
	}
	return nil
}
