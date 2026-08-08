package operatorcfg

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/deploy"
	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/apiexec"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
)

const (
	KeyPolicyOrganization = "policy.layer.organization-10x"
	KeyPolicyPersonal     = "policy.layer.personal-autonomous-venture"
	KeyQuotas             = "quotas"
	KeyMissionDecide      = "mission-decide"
	KeyModelRates         = "executor.model-rates"
	KeyModelPolicy        = "executor.models"
	KeyTunablesValues     = "evolve.tunable-values"
	KeyOpportunityScoring = "opportunity.scoring"
	KeyCatalogAgents      = "packaging.catalog.agents"
	KeyCatalogSkills      = "packaging.catalog.skills"
	KeyEnablePersonal     = "packaging.enablement.personal-autonomous-venture"
	KeyEnableOrganization = "packaging.enablement.organization-10x"
)

// ErrNotConfigured is returned when a store is nil/missing DB.
var ErrNotConfigured = errors.New("operatorcfg: store database is nil")

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type SeedPaths struct {
	PolicyOrganizationPath string
	PolicyPersonalPath     string
	QuotasPath             string
	MissionDecidePath      string
	ModelRatesPath         string
	ModelPolicyPath        string
	OpportunityPath        string
	AgentCatalogPath       string
	SkillCatalogPath       string
	EnablePersonalPath     string
	EnableOrganizationPath string
}

type ApplyMetadata struct {
	ProposalRef string
	ApprovedBy  string
	Reviewer    string
	Implementer string
}

func (s *Store) EnsureSeeded(ctx context.Context, paths SeedPaths) error {
	if s == nil || s.db == nil {
		return ErrNotConfigured
	}
	seedPairs := []struct {
		key  string
		path string
	}{
		{KeyPolicyOrganization, paths.PolicyOrganizationPath},
		{KeyPolicyPersonal, paths.PolicyPersonalPath},
		{KeyQuotas, paths.QuotasPath},
		{KeyMissionDecide, paths.MissionDecidePath},
		{KeyModelRates, paths.ModelRatesPath},
		{KeyModelPolicy, paths.ModelPolicyPath},
		{KeyOpportunityScoring, paths.OpportunityPath},
		{KeyCatalogAgents, paths.AgentCatalogPath},
		{KeyCatalogSkills, paths.SkillCatalogPath},
		{KeyEnablePersonal, paths.EnablePersonalPath},
		{KeyEnableOrganization, paths.EnableOrganizationPath},
	}
	for _, pair := range seedPairs {
		if strings.TrimSpace(pair.path) == "" {
			continue
		}
		raw, err := os.ReadFile(pair.path)
		if err != nil {
			return fmt.Errorf("operatorcfg: read seed %s: %w", pair.path, err)
		}
		if err := s.seedKey(ctx, pair.key, raw); err != nil {
			return err
		}
	}
	// Task 159 seeds tunable values explicitly (empty map by default) so the DB
	// read path exists even before the first promotion writes a value.
	return s.seedKey(ctx, KeyTunablesValues, []byte("values: {}\n"))
}

func (s *Store) seedKey(ctx context.Context, key string, payload []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("operatorcfg: seed %s begin tx: %w", key, err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `
INSERT INTO operator_config_entries (config_key, active_version)
VALUES ($1, 0)
ON CONFLICT (config_key) DO NOTHING`, key); err != nil {
		return fmt.Errorf("operatorcfg: seed %s ensure entry: %w", key, err)
	}
	var active int64
	if err := tx.QueryRowContext(ctx, `SELECT active_version FROM operator_config_entries WHERE config_key = $1 FOR UPDATE`, key).Scan(&active); err != nil {
		return fmt.Errorf("operatorcfg: seed %s lock entry: %w", key, err)
	}
	if active != 0 {
		return tx.Commit()
	}
	digest := sha256Hex(payload)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO operator_config_versions
    (config_key, version, payload, payload_sha256, proposal_ref, approved_by, reviewer, implementer)
VALUES ($1, 1, $2, $3, '', 'seed', '', '')`, key, payload, digest); err != nil {
		return fmt.Errorf("operatorcfg: seed %s insert v1: %w", key, err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE operator_config_entries
SET active_version = 1, updated_at = now()
WHERE config_key = $1`, key); err != nil {
		return fmt.Errorf("operatorcfg: seed %s activate v1: %w", key, err)
	}
	return tx.Commit()
}

func (s *Store) Apply(ctx context.Context, key string, payload []byte, meta ApplyMetadata) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrNotConfigured
	}
	if strings.TrimSpace(meta.ProposalRef) == "" || strings.TrimSpace(meta.ApprovedBy) == "" {
		return 0, fmt.Errorf("operatorcfg: apply %s requires proposal_ref and approved_by", key)
	}
	if strings.TrimSpace(meta.Reviewer) == "" || strings.TrimSpace(meta.Implementer) == "" {
		return 0, fmt.Errorf("operatorcfg: apply %s requires reviewer and implementer", key)
	}
	if meta.Reviewer == meta.Implementer {
		return 0, fmt.Errorf("operatorcfg: apply %s requires reviewer != implementer", key)
	}
	if err := s.validateApplyPayload(ctx, key, payload); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s begin tx: %w", key, err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `
INSERT INTO operator_config_entries (config_key, active_version)
VALUES ($1, 0)
ON CONFLICT (config_key) DO NOTHING`, key); err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s ensure entry: %w", key, err)
	}
	var active int64
	if err := tx.QueryRowContext(ctx, `SELECT active_version FROM operator_config_entries WHERE config_key = $1 FOR UPDATE`, key).Scan(&active); err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s lock entry: %w", key, err)
	}
	next := active + 1
	digest := sha256Hex(payload)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO operator_config_versions
    (config_key, version, payload, payload_sha256, proposal_ref, approved_by, reviewer, implementer)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		key, next, payload, digest, meta.ProposalRef, meta.ApprovedBy, meta.Reviewer, meta.Implementer); err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s insert v%d: %w", key, next, err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE operator_config_entries
SET active_version = $2, updated_at = now()
WHERE config_key = $1`, key, next); err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s activate v%d: %w", key, next, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO operator_config_apply_audit
    (config_key, version, proposal_ref, approved_by, reviewer, implementer)
VALUES ($1, $2, $3, $4, $5, $6)`,
		key, next, meta.ProposalRef, meta.ApprovedBy, meta.Reviewer, meta.Implementer); err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s audit v%d: %w", key, next, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("operatorcfg: apply %s commit: %w", key, err)
	}
	return next, nil
}

func (s *Store) validateApplyPayload(ctx context.Context, key string, payload []byte) error {
	switch key {
	case KeyPolicyOrganization:
		org, _, err := compiler.ParseOrgLayerYAML(payload, "db:"+KeyPolicyOrganization)
		if err != nil {
			return err
		}
		profileRaw, _, _, err := s.LoadActivePayload(ctx, KeyPolicyPersonal)
		if err != nil {
			return err
		}
		profile, err := compiler.ParseProfileLayerYAML(profileRaw, "db:"+KeyPolicyPersonal)
		if err != nil {
			return err
		}
		platform, err := compiler.PlatformDefaults()
		if err != nil {
			return err
		}
		if _, err := compiler.Compile(platform, org, profile, compiler.WorkflowLayer()); err != nil {
			return fmt.Errorf("operatorcfg: apply %s rejected by policy compiler: %w", key, err)
		}
	case KeyPolicyPersonal:
		profile, err := compiler.ParseProfileLayerYAML(payload, "db:"+KeyPolicyPersonal)
		if err != nil {
			return err
		}
		orgRaw, _, _, err := s.LoadActivePayload(ctx, KeyPolicyOrganization)
		if err != nil {
			return err
		}
		org, _, err := compiler.ParseOrgLayerYAML(orgRaw, "db:"+KeyPolicyOrganization)
		if err != nil {
			return err
		}
		platform, err := compiler.PlatformDefaults()
		if err != nil {
			return err
		}
		if _, err := compiler.Compile(platform, org, profile, compiler.WorkflowLayer()); err != nil {
			return fmt.Errorf("operatorcfg: apply %s rejected by policy compiler: %w", key, err)
		}
	case KeyQuotas:
		if _, err := deploy.ParseQuotasYAML(payload, "db:"+KeyQuotas); err != nil {
			return err
		}
	case KeyMissionDecide:
		if _, err := mission.ParseDecidePolicyYAML(payload, "db:"+KeyMissionDecide); err != nil {
			return err
		}
	case KeyModelRates:
		if _, err := cost.ParseRateTableYAML(payload, "db:"+KeyModelRates); err != nil {
			return err
		}
	case KeyModelPolicy:
		if _, err := apiexec.ParseModelPolicyYAML(payload, "db:"+KeyModelPolicy); err != nil {
			return err
		}
	case KeyOpportunityScoring:
		next, err := opportunity.ParseConfigYAML(payload, "db:"+KeyOpportunityScoring)
		if err != nil {
			return err
		}
		baseline, err := opportunity.LoadConfig(opportunity.DefaultConfigPath)
		if err != nil {
			return fmt.Errorf("operatorcfg: load opportunity baseline: %w", err)
		}
		if !sameStringSet(next.PlatformPolicyMarkers, baseline.PlatformPolicyMarkers) ||
			!sameStringSet(next.ValuePropAIMarkers, baseline.ValuePropAIMarkers) ||
			!sameDimensionSources(next.DimensionSources, baseline.DimensionSources) {
			return fmt.Errorf("operatorcfg: apply %s may not change domains/markers topology", key)
		}
	case KeyTunablesValues:
		values, err := parseTunableValueDocument(payload)
		if err != nil {
			return err
		}
		// Load from the repo root relative to this package (internal/operatorcfg)
		registry, err := evolve.LoadTunables("../../config/tunables.yaml")
		if err != nil {
			return fmt.Errorf("operatorcfg: load tunable bounds: %w", err)
		}
		for name, value := range values {
			if !registry.InBounds(name, value) {
				return fmt.Errorf("operatorcfg: apply %s value %q=%g out of bounds", key, name, value)
			}
		}
	case KeyCatalogAgents:
		skillsRaw, _, _, err := s.LoadActivePayload(ctx, KeyCatalogSkills)
		if err != nil {
			return err
		}
		if _, err := packaging.ParseCatalogsYAML(payload, skillsRaw); err != nil {
			return err
		}
	case KeyCatalogSkills:
		agentsRaw, _, _, err := s.LoadActivePayload(ctx, KeyCatalogAgents)
		if err != nil {
			return err
		}
		if _, err := packaging.ParseCatalogsYAML(agentsRaw, payload); err != nil {
			return err
		}
	case KeyEnablePersonal, KeyEnableOrganization:
		if _, err := packaging.ParseEnablementYAML(payload, "db:"+key); err != nil {
			return err
		}
	default:
		return fmt.Errorf("operatorcfg: apply %s is not a supported config key", key)
	}
	return nil
}

func parseTunableValueDocument(raw []byte) (map[string]float64, error) {
	type doc struct {
		Values map[string]float64 `yaml:"values"`
	}
	var out doc
	if err := evolve.ParseTunableValuesYAML(raw, "db:"+KeyTunablesValues, &out); err != nil {
		return nil, err
	}
	if out.Values == nil {
		return map[string]float64{}, nil
	}
	return out.Values, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
		if seen[v] == 0 {
			delete(seen, v)
		}
	}
	return len(seen) == 0
}

func sameDimensionSources(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !sameStringSet(va, vb) {
			return false
		}
	}
	return true
}

func (s *Store) LoadActivePayload(ctx context.Context, key string) ([]byte, int64, string, error) {
	if s == nil || s.db == nil {
		return nil, 0, "", ErrNotConfigured
	}
	const q = `
SELECT v.payload, v.version, v.payload_sha256
FROM operator_config_entries e
JOIN operator_config_versions v
  ON v.config_key = e.config_key
 AND v.version = e.active_version
WHERE e.config_key = $1`
	var payload []byte
	var version int64
	var digest string
	if err := s.db.QueryRowContext(ctx, q, key).Scan(&payload, &version, &digest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, "", fmt.Errorf("operatorcfg: active config %s not found", key)
		}
		return nil, 0, "", fmt.Errorf("operatorcfg: load active %s: %w", key, err)
	}
	if sha256Hex(payload) != digest {
		return nil, 0, "", fmt.Errorf("operatorcfg: active config %s digest mismatch", key)
	}
	return payload, version, digest, nil
}

func (s *Store) LoadEffectivePolicy(ctx context.Context) (*compiler.Resolved, compiler.OrgGovernancePack, error) {
	orgRaw, _, _, err := s.LoadActivePayload(ctx, KeyPolicyOrganization)
	if err != nil {
		return nil, compiler.OrgGovernancePack{}, err
	}
	profileRaw, _, _, err := s.LoadActivePayload(ctx, KeyPolicyPersonal)
	if err != nil {
		return nil, compiler.OrgGovernancePack{}, err
	}
	org, pack, err := compiler.ParseOrgLayerYAML(orgRaw, "db:"+KeyPolicyOrganization)
	if err != nil {
		return nil, compiler.OrgGovernancePack{}, err
	}
	profile, err := compiler.ParseProfileLayerYAML(profileRaw, "db:"+KeyPolicyPersonal)
	if err != nil {
		return nil, compiler.OrgGovernancePack{}, err
	}
	platform, err := compiler.PlatformDefaults()
	if err != nil {
		return nil, compiler.OrgGovernancePack{}, err
	}
	resolved, err := compiler.Compile(platform, org, profile, compiler.WorkflowLayer())
	if err != nil {
		return nil, compiler.OrgGovernancePack{}, err
	}
	return resolved, pack, nil
}

func (s *Store) LoadQuotas(ctx context.Context) (map[string]deploy.ProfileQuota, error) {
	raw, _, _, err := s.LoadActivePayload(ctx, KeyQuotas)
	if err != nil {
		return nil, err
	}
	return deploy.ParseQuotasYAML(raw, "db:"+KeyQuotas)
}

func (s *Store) LoadMissionDecidePolicy(ctx context.Context) (mission.DecidePolicy, error) {
	raw, _, _, err := s.LoadActivePayload(ctx, KeyMissionDecide)
	if err != nil {
		return mission.DecidePolicy{}, err
	}
	return mission.ParseDecidePolicyYAML(raw, "db:"+KeyMissionDecide)
}

func (s *Store) LoadModelRates(ctx context.Context) (cost.RateTable, error) {
	raw, _, _, err := s.LoadActivePayload(ctx, KeyModelRates)
	if err != nil {
		return cost.RateTable{}, err
	}
	return cost.ParseRateTableYAML(raw, "db:"+KeyModelRates)
}

func (s *Store) LoadModelPolicy(ctx context.Context) (apiexec.ModelPolicy, error) {
	raw, _, _, err := s.LoadActivePayload(ctx, KeyModelPolicy)
	if err != nil {
		return apiexec.ModelPolicy{}, err
	}
	return apiexec.ParseModelPolicyYAML(raw, "db:"+KeyModelPolicy)
}

func (s *Store) LoadOpportunityConfig(ctx context.Context) (opportunity.Config, error) {
	raw, _, _, err := s.LoadActivePayload(ctx, KeyOpportunityScoring)
	if err != nil {
		return opportunity.Config{}, err
	}
	return opportunity.ParseConfigYAML(raw, "db:"+KeyOpportunityScoring)
}

func (s *Store) LoadCatalogs(ctx context.Context) (packaging.Catalogs, error) {
	agents, _, _, err := s.LoadActivePayload(ctx, KeyCatalogAgents)
	if err != nil {
		return packaging.Catalogs{}, err
	}
	skills, _, _, err := s.LoadActivePayload(ctx, KeyCatalogSkills)
	if err != nil {
		return packaging.Catalogs{}, err
	}
	return packaging.ParseCatalogsYAML(agents, skills)
}

func (s *Store) LoadEnablement(ctx context.Context, profile string) (packaging.Enablement, error) {
	key := KeyEnablePersonal
	switch profile {
	case "organization-10x":
		key = KeyEnableOrganization
	case "personal-autonomous-venture":
		key = KeyEnablePersonal
	default:
		return packaging.Enablement{}, fmt.Errorf("operatorcfg: unsupported enablement profile %q", profile)
	}
	raw, _, _, err := s.LoadActivePayload(ctx, key)
	if err != nil {
		return packaging.Enablement{}, err
	}
	return packaging.ParseEnablementYAML(raw, "db:"+key)
}

// LoadProfileEnablement reads profile package declarations from DB-backed
// policy payloads (agent_packages / skill_packages subsets only).
func (s *Store) LoadProfileEnablement(ctx context.Context, profile string) (packaging.ProfileEnablement, error) {
	var key string
	switch profile {
	case "organization-10x":
		key = KeyPolicyOrganization
	case "personal-autonomous-venture":
		key = KeyPolicyPersonal
	default:
		return packaging.ProfileEnablement{}, fmt.Errorf("operatorcfg: unsupported profile %q", profile)
	}
	raw, _, _, err := s.LoadActivePayload(ctx, key)
	if err != nil {
		return packaging.ProfileEnablement{}, err
	}
	return packaging.ParseProfileEnablementYAML(raw, "db:"+key)
}

// LoadTunableValues returns L0 effective tunable values from DB.
func (s *Store) LoadTunableValues(ctx context.Context) (map[string]float64, error) {
	raw, _, _, err := s.LoadActivePayload(ctx, KeyTunablesValues)
	if err != nil {
		return nil, err
	}
	return parseTunableValueDocument(raw)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
