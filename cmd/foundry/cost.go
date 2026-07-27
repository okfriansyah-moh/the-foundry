package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

const costCmdTimeout = 10 * time.Second

// parseScopeArg splits a "scope:scope_id" argument (e.g. "mission:m1",
// "workflow:wf-42") into its two parts, matching cost.Scope's registered
// values (workflow, product, mission).
func parseScopeArg(raw string) (cost.Scope, string, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid --scope %q: want \"<scope>:<scope-id>\" e.g. \"mission:m1\"", raw)
	}
	scope := cost.Scope(parts[0])
	switch scope {
	case cost.ScopeWorkflow, cost.ScopeProduct, cost.ScopeMission:
	default:
		return "", "", fmt.Errorf("invalid --scope %q: scope must be one of workflow, product, mission", raw)
	}
	return scope, parts[1], nil
}

// runCostShow implements `foundry cost show --scope <scope>:<id>`
// (docs/PLAN.md Task 29/FND-10): lists every budget envelope provisioned
// for that scope, with each envelope's ceiling and running totals.
func runCostShow(args []string) error {
	fs := flag.NewFlagSet("cost show", flag.ContinueOnError)
	scopeArg := fs.String("scope", "", "scope to show, as \"<scope>:<id>\" e.g. \"mission:m1\" (required)")
	pgDSN := fs.String("pg-dsn", os.Getenv("PG_DSN"), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scopeArg == "" {
		return errors.New("cost show: --scope is required")
	}
	scope, scopeID, err := parseScopeArg(*scopeArg)
	if err != nil {
		return fmt.Errorf("cost show: %w", err)
	}
	if *pgDSN == "" {
		return errors.New("cost show: no --pg-dsn/PG_DSN set")
	}

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("cost show: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), costCmdTimeout)
	defer cancel()

	store := cost.NewStore(db)
	budgets, err := store.ListBudgets(ctx, scope, scopeID)
	if err != nil {
		return fmt.Errorf("cost show: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(budgets)
}
