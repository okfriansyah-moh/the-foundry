package identity

import (
	"fmt"
	"time"
)

// PrincipalKind enumerates the values internal/db/migrations/00004_principals.sql
// constrains principals.kind to (CHECK (kind IN ('human', 'service'))).
type PrincipalKind string

const (
	PrincipalHuman   PrincipalKind = "human"
	PrincipalService PrincipalKind = "service"
)

// Valid reports whether k is one of the kinds the principals table accepts.
func (k PrincipalKind) Valid() bool {
	switch k {
	case PrincipalHuman, PrincipalService:
		return true
	default:
		return false
	}
}

// Principal is the typed view of a principals row.
type Principal struct {
	ID         string
	Kind       PrincipalKind
	Display    string
	IDPSubject *string // nullable idp_subject column
	CreatedAt  time.Time
}

// Validate checks p against the constraints the DB schema enforces, so
// callers fail fast with a field-scoped error instead of a bare constraint
// violation from Postgres.
func (p *Principal) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("identity: principal id required")
	}
	if !p.Kind.Valid() {
		return fmt.Errorf("identity: principal kind %q invalid (want human|service)", p.Kind)
	}
	if p.Display == "" {
		return fmt.Errorf("identity: principal display required")
	}
	return nil
}

// Organization is the typed view of an organizations row.
type Organization struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Validate checks o against the constraints the DB schema enforces.
func (o *Organization) Validate() error {
	if o.ID == "" {
		return fmt.Errorf("identity: organization id required")
	}
	if o.Name == "" {
		return fmt.Errorf("identity: organization name required")
	}
	return nil
}

// OrgMember is the typed view of an org_members row: a principal's role
// within one organization. (org_id, principal_id) is the primary key, so a
// principal has at most one role per organization.
type OrgMember struct {
	OrgID       string
	PrincipalID string
	Role        string
	CreatedAt   time.Time
}

// Validate checks m against the constraints the DB schema enforces.
func (m *OrgMember) Validate() error {
	if m.OrgID == "" {
		return fmt.Errorf("identity: org member org_id required")
	}
	if m.PrincipalID == "" {
		return fmt.Errorf("identity: org member principal_id required")
	}
	if m.Role == "" {
		return fmt.Errorf("identity: org member role required")
	}
	return nil
}
