package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/projection"
)

const projectionCmdTimeout = 60 * time.Second

// runProjectionRebuild implements `foundry projection rebuild` (docs/PLAN.md
// Task 14 Step 3): truncate workflow_status_projection and replay every
// transition from seq 0, asserting row-count completeness and printing the
// digest computed by projection_checksum() (internal/db/migrations/00003_projection.sql)
// for the caller to compare against a value captured before the drop.
func runProjectionRebuild(args []string) error {
	fs := flag.NewFlagSet("projection rebuild", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", "", "Postgres DSN (defaults to $PG_DSN)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dsn := *pgDSN
	if dsn == "" {
		dsn = os.Getenv("PG_DSN")
	}
	if dsn == "" {
		dsn = "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("projection rebuild: open postgres: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), projectionCmdTimeout)
	defer cancel()

	result, err := projection.Rebuild(ctx, db, projection.DefaultProjectorName)
	if err != nil {
		return fmt.Errorf("projection rebuild: %w", err)
	}

	fmt.Printf("PASS: projection rebuilt — rows=%d checksum=%s\n", result.Rows, result.Checksum)
	return nil
}
