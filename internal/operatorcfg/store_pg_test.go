package operatorcfg

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("PG_DSN set but unreachable: %v", err)
	}
	migrator, err := db.NewMigrator(sqlDB)
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	if err := migrator.Up(context.Background()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return sqlDB
}

func seedPaths() SeedPaths {
	return SeedPaths{
		PolicyOrganizationPath: filepath.Join("..", "..", "config", "profiles", "organization-10x.yaml"),
		PolicyPersonalPath:     filepath.Join("..", "..", "config", "profiles", "personal-autonomous-venture.yaml"),
		QuotasPath:             filepath.Join("..", "..", "config", "quotas.yaml"),
		MissionDecidePath:      filepath.Join("..", "..", "config", "mission-decide-policy.yaml"),
		ModelRatesPath:         filepath.Join("..", "..", "config", "executor-model-rates.yaml"),
		ModelPolicyPath:        filepath.Join("..", "..", "config", "executor-models.yaml"),
		OpportunityPath:        filepath.Join("..", "..", "config", "opportunity-thresholds.yaml"),
		AgentCatalogPath:       filepath.Join("..", "..", "agents", "catalog.yaml"),
		SkillCatalogPath:       filepath.Join("..", "..", "skills", "catalog.yaml"),
		EnablePersonalPath:     filepath.Join("..", "..", "templates", "product", ".foundry", "skills", "enabled.yaml"),
		EnableOrganizationPath: filepath.Join("..", "..", "templates", "product", ".foundry", "skills", "enabled.yaml"),
	}
}

func TestApplyRejectsReviewerImplementerCollision(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	if err := store.EnsureSeeded(context.Background(), seedPaths()); err != nil {
		t.Fatalf("ensure seeded: %v", err)
	}
	_, err := store.Apply(context.Background(), KeyQuotas, []byte("profiles: {}\n"), ApplyMetadata{
		ProposalRef: "proposal-1",
		ApprovedBy:  "approver",
		Reviewer:    "same",
		Implementer: "same",
	})
	if err == nil || !strings.Contains(err.Error(), "reviewer != implementer") {
		t.Fatalf("apply error = %v, want reviewer!=implementer rejection", err)
	}
}

func TestApplyRejectsLoosenedPolicyLayer(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	if err := store.EnsureSeeded(context.Background(), seedPaths()); err != nil {
		t.Fatalf("ensure seeded: %v", err)
	}
	_, err := store.Apply(context.Background(), KeyPolicyOrganization, []byte("executor_allowlist:\n  - totally-unknown\n"), ApplyMetadata{
		ProposalRef: "proposal-2",
		ApprovedBy:  "approver",
		Reviewer:    "reviewer",
		Implementer: "implementer",
	})
	if err == nil || !strings.Contains(err.Error(), "rejected by policy compiler") {
		t.Fatalf("apply error = %v, want loosen rejection", err)
	}
}

func TestApplyRejectsOutOfBoundsTunableValue(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	if err := store.EnsureSeeded(context.Background(), seedPaths()); err != nil {
		t.Fatalf("ensure seeded: %v", err)
	}
	_, err := store.Apply(context.Background(), KeyTunablesValues, []byte("values:\n  wave_concurrency: 999999\n"), ApplyMetadata{
		ProposalRef: "proposal-3",
		ApprovedBy:  "approver",
		Reviewer:    "reviewer",
		Implementer: "implementer",
	})
	if err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("apply error = %v, want out-of-bounds rejection", err)
	}
}

func TestApplyWritesAuditRow(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	if err := store.EnsureSeeded(context.Background(), seedPaths()); err != nil {
		t.Fatalf("ensure seeded: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join("..", "..", "config", "quotas.yaml"))
	if err != nil {
		t.Fatalf("read quotas: %v", err)
	}
	version, err := store.Apply(context.Background(), KeyQuotas, payload, ApplyMetadata{
		ProposalRef: "proposal-4",
		ApprovedBy:  "approver",
		Reviewer:    "reviewer",
		Implementer: "implementer",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM operator_config_apply_audit WHERE config_key = $1 AND version = $2`, KeyQuotas, version).Scan(&count); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit rows = %d, want 1", count)
	}
}
