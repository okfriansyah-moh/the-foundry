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

// projectionRolloutCmdTimeout is longer than rebuild's: a rollout backfills
// a full shadow table from seq 0 and may retry the checksum-compare loop
// several times against a live, still-changing table (docs/PLAN.md Task 38).
const projectionRolloutCmdTimeout = 5 * time.Minute

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
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), projectionCmdTimeout)
	defer cancel()

	result, err := projection.Rebuild(ctx, db, projection.DefaultProjectorName)
	if err != nil {
		return fmt.Errorf("projection rebuild: %w", err)
	}

	fmt.Printf("PASS: projection rebuilt — rows=%d checksum=%s\n", result.Rows, result.Checksum)
	return nil
}

// runProjectionRollout implements `foundry projection rollout` (docs/PLAN.md
// Task 38 / FND-19): build the --to-version projector into a shadow table
// via full replay from seq 0, converge it against the live table's content
// (checksum-compare, bounded retries), then atomically swap it into place.
// See internal/projection/versioning.go's Rollout for the full algorithm
// and its zero-update-loss argument.
func runProjectionRollout(args []string) error {
	fs := flag.NewFlagSet("projection rollout", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", "", "Postgres DSN (defaults to $PG_DSN)")
	toVersion := fs.String("to-version", "", "target projector_version to stamp the new shadow-table rows with (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *toVersion == "" {
		return fmt.Errorf("projection rollout: --to-version is required")
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
		return fmt.Errorf("projection rollout: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), projectionRolloutCmdTimeout)
	defer cancel()

	result, err := projection.Rollout(ctx, db, *toVersion)
	if err != nil {
		return fmt.Errorf("projection rollout: %w", err)
	}

	fmt.Printf("PASS: projection rolled out %s -> %s — rows=%d checksum=%s converge_attempts=%d\n",
		result.FromVersion, result.ToVersion, result.Rows, result.Checksum, result.ConvergeAttempts)
	return nil
}
