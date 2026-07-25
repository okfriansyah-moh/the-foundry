package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver registration
)

// ErrNotFound is returned when a lookup by ID finds no row.
var ErrNotFound = errors.New("identity: not found")

// ErrAlreadyExists is returned by a Create call whose ID is already taken,
// so idempotent callers (e.g. test/fixtures/seed_profiles.go) can detect
// "already seeded" without a database-specific error type.
var ErrAlreadyExists = errors.New("identity: already exists")

// Store is the CRUD surface over principals, organizations, and
// org_members. MemStore is an in-memory fake for tests and any run without
// a live Postgres; PGStore is the real Postgres-backed implementation.
type Store interface {
	CreatePrincipal(ctx context.Context, p *Principal) error
	GetPrincipal(ctx context.Context, id string) (*Principal, error)
	ListPrincipals(ctx context.Context) ([]*Principal, error)

	CreateOrganization(ctx context.Context, o *Organization) error
	GetOrganization(ctx context.Context, id string) (*Organization, error)
	ListOrganizations(ctx context.Context) ([]*Organization, error)

	AddOrgMember(ctx context.Context, m *OrgMember) error
	ListOrgMembers(ctx context.Context, orgID string) ([]*OrgMember, error)
}

// MemStore is an in-memory Store for tests and for any run without a live
// Postgres (mirrors internal/provenance's RawStore/MemRawStore split).
type MemStore struct {
	principals    map[string]*Principal
	organizations map[string]*Organization
	orgMembers    map[string]*OrgMember // keyed by orgID+"/"+principalID
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		principals:    make(map[string]*Principal),
		organizations: make(map[string]*Organization),
		orgMembers:    make(map[string]*OrgMember),
	}
}

// CreatePrincipal implements Store.
func (m *MemStore) CreatePrincipal(_ context.Context, p *Principal) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if _, exists := m.principals[p.ID]; exists {
		return fmt.Errorf("%w: principal %s", ErrAlreadyExists, p.ID)
	}
	cp := *p
	m.principals[p.ID] = &cp
	return nil
}

// GetPrincipal implements Store.
func (m *MemStore) GetPrincipal(_ context.Context, id string) (*Principal, error) {
	p, ok := m.principals[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

// ListPrincipals implements Store, sorted by ID for deterministic output.
func (m *MemStore) ListPrincipals(_ context.Context) ([]*Principal, error) {
	out := make([]*Principal, 0, len(m.principals))
	for _, p := range m.principals {
		cp := *p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// CreateOrganization implements Store.
func (m *MemStore) CreateOrganization(_ context.Context, o *Organization) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if _, exists := m.organizations[o.ID]; exists {
		return fmt.Errorf("%w: organization %s", ErrAlreadyExists, o.ID)
	}
	cp := *o
	m.organizations[o.ID] = &cp
	return nil
}

// GetOrganization implements Store.
func (m *MemStore) GetOrganization(_ context.Context, id string) (*Organization, error) {
	o, ok := m.organizations[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *o
	return &cp, nil
}

// ListOrganizations implements Store, sorted by ID for deterministic output.
func (m *MemStore) ListOrganizations(_ context.Context) ([]*Organization, error) {
	out := make([]*Organization, 0, len(m.organizations))
	for _, o := range m.organizations {
		cp := *o
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// AddOrgMember implements Store.
func (m *MemStore) AddOrgMember(_ context.Context, om *OrgMember) error {
	if err := om.Validate(); err != nil {
		return err
	}
	if _, ok := m.organizations[om.OrgID]; !ok {
		return fmt.Errorf("identity: org member references unknown organization %s", om.OrgID)
	}
	if _, ok := m.principals[om.PrincipalID]; !ok {
		return fmt.Errorf("identity: org member references unknown principal %s", om.PrincipalID)
	}
	key := om.OrgID + "/" + om.PrincipalID
	if _, exists := m.orgMembers[key]; exists {
		return fmt.Errorf("%w: org member %s", ErrAlreadyExists, key)
	}
	cp := *om
	m.orgMembers[key] = &cp
	return nil
}

// ListOrgMembers implements Store, sorted by principal ID for deterministic
// output.
func (m *MemStore) ListOrgMembers(_ context.Context, orgID string) ([]*OrgMember, error) {
	out := make([]*OrgMember, 0)
	for _, om := range m.orgMembers {
		if om.OrgID == orgID {
			cp := *om
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrincipalID < out[j].PrincipalID })
	return out, nil
}

// PGStore is the Postgres-backed Store
// (internal/db/migrations/00004_principals.sql). All queries are
// parameterized — no string-built SQL, no injection surface.
type PGStore struct {
	db *sql.DB
}

// OpenPGStore opens a PGStore against dsn using the pgx database/sql driver.
func OpenPGStore(dsn string) (*PGStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("identity: open postgres: %w", err)
	}
	return &PGStore{db: db}, nil
}

// Close closes the underlying connection pool.
func (p *PGStore) Close() error { return p.db.Close() }

// CreatePrincipal implements Store with a parameterized INSERT.
func (p *PGStore) CreatePrincipal(ctx context.Context, pr *Principal) error {
	if err := pr.Validate(); err != nil {
		return err
	}
	const q = `INSERT INTO principals (id, kind, display, idp_subject) VALUES ($1, $2, $3, $4)`
	if _, err := p.db.ExecContext(ctx, q, pr.ID, string(pr.Kind), pr.Display, pr.IDPSubject); err != nil {
		return fmt.Errorf("identity: insert principal %s: %w", pr.ID, err)
	}
	return nil
}

// GetPrincipal implements Store with a parameterized SELECT.
func (p *PGStore) GetPrincipal(ctx context.Context, id string) (*Principal, error) {
	const q = `SELECT id, kind, display, idp_subject, created_at FROM principals WHERE id = $1`
	pr := &Principal{}
	err := p.db.QueryRowContext(ctx, q, id).Scan(&pr.ID, &pr.Kind, &pr.Display, &pr.IDPSubject, &pr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("identity: load principal %s: %w", id, err)
	}
	return pr, nil
}

// ListPrincipals implements Store with a parameterized SELECT, ordered by id.
func (p *PGStore) ListPrincipals(ctx context.Context) ([]*Principal, error) {
	const q = `SELECT id, kind, display, idp_subject, created_at FROM principals ORDER BY id`
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("identity: list principals: %w", err)
	}
	defer rows.Close()

	var out []*Principal
	for rows.Next() {
		pr := &Principal{}
		if err := rows.Scan(&pr.ID, &pr.Kind, &pr.Display, &pr.IDPSubject, &pr.CreatedAt); err != nil {
			return nil, fmt.Errorf("identity: scan principal: %w", err)
		}
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list principals: %w", err)
	}
	return out, nil
}

// CreateOrganization implements Store with a parameterized INSERT.
func (p *PGStore) CreateOrganization(ctx context.Context, o *Organization) error {
	if err := o.Validate(); err != nil {
		return err
	}
	const q = `INSERT INTO organizations (id, name) VALUES ($1, $2)`
	if _, err := p.db.ExecContext(ctx, q, o.ID, o.Name); err != nil {
		return fmt.Errorf("identity: insert organization %s: %w", o.ID, err)
	}
	return nil
}

// GetOrganization implements Store with a parameterized SELECT.
func (p *PGStore) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	const q = `SELECT id, name, created_at FROM organizations WHERE id = $1`
	o := &Organization{}
	err := p.db.QueryRowContext(ctx, q, id).Scan(&o.ID, &o.Name, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("identity: load organization %s: %w", id, err)
	}
	return o, nil
}

// ListOrganizations implements Store with a parameterized SELECT, ordered by id.
func (p *PGStore) ListOrganizations(ctx context.Context) ([]*Organization, error) {
	const q = `SELECT id, name, created_at FROM organizations ORDER BY id`
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("identity: list organizations: %w", err)
	}
	defer rows.Close()

	var out []*Organization
	for rows.Next() {
		o := &Organization{}
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("identity: scan organization: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list organizations: %w", err)
	}
	return out, nil
}

// AddOrgMember implements Store with a parameterized INSERT.
func (p *PGStore) AddOrgMember(ctx context.Context, m *OrgMember) error {
	if err := m.Validate(); err != nil {
		return err
	}
	const q = `INSERT INTO org_members (org_id, principal_id, role) VALUES ($1, $2, $3)`
	if _, err := p.db.ExecContext(ctx, q, m.OrgID, m.PrincipalID, m.Role); err != nil {
		return fmt.Errorf("identity: insert org member %s/%s: %w", m.OrgID, m.PrincipalID, err)
	}
	return nil
}

// ListOrgMembers implements Store with a parameterized SELECT, ordered by
// principal_id.
func (p *PGStore) ListOrgMembers(ctx context.Context, orgID string) ([]*OrgMember, error) {
	const q = `SELECT org_id, principal_id, role, created_at FROM org_members WHERE org_id = $1 ORDER BY principal_id`
	rows, err := p.db.QueryContext(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("identity: list org members for %s: %w", orgID, err)
	}
	defer rows.Close()

	var out []*OrgMember
	for rows.Next() {
		m := &OrgMember{}
		if err := rows.Scan(&m.OrgID, &m.PrincipalID, &m.Role, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("identity: scan org member: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list org members for %s: %w", orgID, err)
	}
	return out, nil
}
