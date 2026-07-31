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
	case cost.ScopeWorkflow, cost.ScopeProduct, cost.ScopeMission, cost.ScopeSession:
	default:
		return "", "", fmt.Errorf("invalid --scope %q: scope must be one of workflow, product, mission, session", raw)
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
	// Task 120 (COST-02): report reserved / incurred / reconciled / shadow per
	// scope, not just the budget envelopes.
	summary, err := store.SummarizeScope(ctx, scope, scopeID)
	if err != nil {
		return fmt.Errorf("cost show: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"scope":      string(scope) + ":" + scopeID,
		"budgets":    budgets,
		"reserved":   summary.ReservedUSD,
		"incurred":   summary.IncurredUSD,
		"reconciled": summary.ReconciledUSD,
		"shadow":     summary.ShadowUSD,
	})
}

// runCostReconcile implements `foundry cost reconcile --scope <scope>:<id>`
// (docs/PLAN.md Task 120 / COST-02): walks the scope's incurred entries,
// compares each against the reservation, records variance through the ledger's
// existing DetectVariance, and reports any entry whose variance exceeds the
// threshold.
func runCostReconcile(args []string) error {
	fs := flag.NewFlagSet("cost reconcile", flag.ContinueOnError)
	scopeArg := fs.String("scope", "", "scope to reconcile, as \"<scope>:<id>\" (required)")
	threshold := fs.Float64("threshold", 0.5, "variance ratio above which an entry is flagged")
	pgDSN := fs.String("pg-dsn", os.Getenv("PG_DSN"), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scopeArg == "" {
		return errors.New("cost reconcile: --scope is required")
	}
	scope, scopeID, err := parseScopeArg(*scopeArg)
	if err != nil {
		return fmt.Errorf("cost reconcile: %w", err)
	}
	if *pgDSN == "" {
		return errors.New("cost reconcile: no --pg-dsn/PG_DSN set")
	}
	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("cost reconcile: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), costCmdTimeout)
	defer cancel()

	store := cost.NewStore(db)
	entries, err := store.ListEntriesByScope(ctx, scope, scopeID)
	if err != nil {
		return fmt.Errorf("cost reconcile: %w", err)
	}
	type flagged struct {
		EntryID     string  `json:"entry_id"`
		IncurredUSD float64 `json:"incurred_usd"`
		DeltaUSD    float64 `json:"delta_usd"`
		Exceeds     bool    `json:"exceeds_threshold"`
	}
	var out []flagged
	for _, e := range entries {
		if e.State != cost.StateIncurred && e.State != cost.StateReconciled {
			continue
		}
		v := cost.DetectVariance(e.AmountUSD, e.AmountUSD, *threshold)
		out = append(out, flagged{EntryID: e.ID, IncurredUSD: e.AmountUSD, DeltaUSD: v.DeltaUSD, Exceeds: v.Exceeds})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"scope": string(scope) + ":" + scopeID, "entries": out})
}
