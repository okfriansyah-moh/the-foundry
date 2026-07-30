package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity/report"
)

// docs/PLAN.md Task 103 (OPP-04): read-only CLI over opportunity evidence.
// These commands render and read; they never issue a verdict, transition or
// approval (Constitution C11/C23).

const opportunityCmdTimeout = 30 * time.Second

func openOpportunityStore() (*sql.DB, *opportunity.Store, error) {
	sqlDB, err := sql.Open("pgx", pgDSNFromEnv())
	if err != nil {
		return nil, nil, fmt.Errorf("opportunity: open db: %w", err)
	}
	return sqlDB, opportunity.NewStore(sqlDB), nil
}

func runOpportunityList(args []string) error {
	fs := flag.NewFlagSet("opportunity list", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "maximum rows to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sqlDB, store, err := openOpportunityStore()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), opportunityCmdTimeout)
	defer cancel()

	rows, err := store.ListOpportunities(ctx, *limit)
	if err != nil {
		return err
	}
	fmt.Printf("%-40s  %-20s  %s\n", "ID", "CREATED", "STATEMENT")
	for _, r := range rows {
		fmt.Printf("%-40s  %-20s  %s\n", r.ID, r.CreatedAt.UTC().Format(time.RFC3339), truncate(r.Statement, 60))
	}
	return nil
}

func runOpportunityShow(args []string) error {
	fs := flag.NewFlagSet("opportunity show", flag.ContinueOnError)
	id := fs.String("id", "", "opportunity id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("opportunity show: --id is required")
	}
	sqlDB, store, err := openOpportunityStore()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), opportunityCmdTimeout)
	defer cancel()

	opp, err := store.LoadOpportunity(ctx, *id)
	if err != nil {
		return err
	}
	fmt.Printf("Opportunity %s\n  statement: %s\n  claims: %d\n  validation cost: $%.2f\n  mvp budget: $%.2f\n",
		opp.Idea.ID, opp.Idea.Statement, len(opp.Claims), opp.EstimatedValidationCostUSD, opp.MVPBudgetUSD)
	rec, err := store.LatestVerdict(ctx, *id)
	if err != nil {
		if err == opportunity.ErrNotFound {
			fmt.Println("  verdict: none recorded")
			return nil
		}
		return err
	}
	fmt.Printf("  verdict: %s (config %s)\n  unmet: %v\n", rec.Verdict, rec.ConfigVersion, rec.UnmetThresholds)
	return nil
}

func runOpportunityReport(args []string) error {
	fs := flag.NewFlagSet("opportunity report", flag.ContinueOnError)
	id := fs.String("id", "", "opportunity id")
	out := fs.String("out", "", "optional directory to write the artifact bundle into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("opportunity report: --id is required")
	}
	cfg, err := opportunity.LoadConfig(envOrDefaultOppConfig())
	if err != nil {
		return err
	}
	sqlDB, store, err := openOpportunityStore()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), opportunityCmdTimeout)
	defer cancel()

	opp, err := store.LoadOpportunity(ctx, *id)
	if err != nil {
		return err
	}
	sc := opportunity.Score(opp, cfg)
	verdict, unmet := opportunity.Decide(sc, cfg.Thresholds)
	in := report.Input{
		Opportunity:     opp,
		Scorecard:       sc,
		Verdict:         verdict,
		UnmetThresholds: unmet,
		GeneratedAt:     time.Now().UTC(),
	}
	if *out != "" {
		bundle, err := report.WriteBundle(*out, in)
		if err != nil {
			return err
		}
		fmt.Printf("wrote %d artifacts to %s\n", len(bundle.Manifest.Artifacts), bundle.Dir)
		return nil
	}
	arts, err := report.Render(in)
	if err != nil {
		return err
	}
	for _, a := range arts {
		fmt.Printf("===== %s =====\n%s\n", a.Name, string(a.Content))
	}
	return nil
}

func envOrDefaultOppConfig() string {
	if v := os.Getenv("FOUNDRY_OPPORTUNITY_THRESHOLDS"); v != "" {
		return v
	}
	return "config/opportunity-thresholds.yaml"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
