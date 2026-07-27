package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/db"
)

const migrateTimeout = 30 * time.Second

// pgDSNFromEnv mirrors runDoctor's default so `foundry migrate` works
// against the same compose-network Postgres without extra flags.
func pgDSNFromEnv() string {
	if dsn := os.Getenv("PG_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable"
}

func openMigrator() (*sql.DB, *db.Migrator, error) {
	sqlDB, err := sql.Open("pgx", pgDSNFromEnv())
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: open db: %w", err)
	}
	migrator, err := db.NewMigrator(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("migrate: construct migrator: %w", err)
	}
	return sqlDB, migrator, nil
}

func runMigrateUp(_ []string) error {
	sqlDB, migrator, err := openMigrator()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	if err := migrator.Up(ctx); err != nil {
		return err
	}
	fmt.Println("migrate up: OK")
	return nil
}

func runMigrateDown(_ []string) error {
	sqlDB, migrator, err := openMigrator()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	if err := migrator.Down(ctx); err != nil {
		return err
	}
	fmt.Println("migrate down: OK")
	return nil
}

func runMigrateStatus(_ []string) error {
	sqlDB, migrator, err := openMigrator()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	status, err := migrator.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range status {
		fmt.Printf("%s\t%s\n", s.Source.Path, s.State)
	}
	return nil
}
