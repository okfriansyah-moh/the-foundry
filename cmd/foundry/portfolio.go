package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

const portfolioCmdTimeout = 10 * time.Second

// defaultPortfolioID mirrors cmd/foundryd's default supervised portfolio, so a
// plain `foundry portfolio show` reads the portfolio foundryd runs.
const defaultPortfolioID = "default"

func openPortfolioStore(dsn string) (*mission.PortfolioStore, *sql.DB, error) {
	if dsn == "" {
		return nil, nil, fmt.Errorf("portfolio: no Postgres DSN (set --pg-dsn or PG_DSN)")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("portfolio: open postgres: %w", err)
	}
	return mission.NewPortfolioStore(db), db, nil
}

// runPortfolioShow implements `foundry portfolio show` (docs/PLAN.md Task 121):
// it renders the real, Postgres-backed portfolio digest — the same
// FormatPortfolioDigest the veto digest panel uses, now fed live data rather
// than a test fixture.
func runPortfolioShow(args []string) error {
	fs := flag.NewFlagSet("portfolio show", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	id := fs.String("id", defaultPortfolioID, "portfolio id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, db, err := openPortfolioStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), portfolioCmdTimeout)
	defer cancel()
	p, err := store.Load(ctx, *id)
	if err != nil {
		return fmt.Errorf("portfolio show: %w", err)
	}
	fmt.Print(mission.FormatPortfolioDigest(p))
	return nil
}

// runPortfolioList implements `foundry portfolio list`: a machine-readable
// (JSON) dump of the portfolio's per-mission rows for scripting and status
// tooling.
func runPortfolioList(args []string) error {
	fs := flag.NewFlagSet("portfolio list", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	id := fs.String("id", defaultPortfolioID, "portfolio id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, db, err := openPortfolioStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), portfolioCmdTimeout)
	defer cancel()
	p, err := store.Load(ctx, *id)
	if err != nil {
		return fmt.Errorf("portfolio list: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		PortfolioID    string                    `json:"portfolio_id"`
		ActiveCount    int                       `json:"active_count"`
		Cap            int                       `json:"cap"`
		FairnessSpread int                       `json:"fairness_spread"`
		Missions       []mission.MissionPanelRow `json:"missions"`
	}{
		PortfolioID:    *id,
		ActiveCount:    p.ActiveCount(),
		Cap:            p.MaxActiveProducts,
		FairnessSpread: p.FairnessSpread(),
		Missions:       p.Panel(),
	})
}
