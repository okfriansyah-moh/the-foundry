package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator wraps a goose.Provider configured for this repo's embedded
// migration set and the Postgres dialect.
type Migrator struct {
	provider *goose.Provider
}

// NewMigrator constructs a Migrator against db using the embedded
// migration files. db must already be opened with a Postgres-compatible
// database/sql driver (e.g. github.com/jackc/pgx/v5/stdlib).
func NewMigrator(sqlDB *sql.DB) (*Migrator, error) {
	migrationsRoot, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: sub embedded migrations dir: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrationsRoot)
	if err != nil {
		return nil, fmt.Errorf("db: construct goose provider: %w", err)
	}
	return &Migrator{provider: provider}, nil
}

// Up applies all pending migrations, in order.
func (m *Migrator) Up(ctx context.Context) error {
	if _, err := m.provider.Up(ctx); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}

// Down rolls back exactly one migration (the most recently applied).
func (m *Migrator) Down(ctx context.Context) error {
	if _, err := m.provider.Down(ctx); err != nil {
		return fmt.Errorf("db: migrate down: %w", err)
	}
	return nil
}

// Status reports the applied/pending state of every known migration.
func (m *Migrator) Status(ctx context.Context) ([]*goose.MigrationStatus, error) {
	status, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: migrate status: %w", err)
	}
	return status, nil
}
