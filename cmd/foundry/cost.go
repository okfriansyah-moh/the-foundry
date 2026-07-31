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
// compares each recorded amount against an authoritative provider-reported
// figure (from --observed), records variance through the ledger's existing
// DetectVariance, and reports any entry whose absolute USD variance exceeds
// the threshold. Entries with no observed figure are reported but never
// flagged — a missing figure is not a variance.
func runCostReconcile(args []string) error {
	fs := flag.NewFlagSet("cost reconcile", flag.ContinueOnError)
	scopeArg := fs.String("scope", "", "scope to reconcile, as \"<scope>:<id>\" (required)")
	threshold := fs.Float64("threshold", 0.5, "absolute USD variance (|observed - recorded|) above which an entry is flagged")
	observedPath := fs.String("observed", "", "path to a JSON file mapping entry_id -> provider-reported USD (the authoritative figures to reconcile against)")
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
	observed, err := loadObservedFigures(*observedPath)
	if err != nil {
		return fmt.Errorf("cost reconcile: %w", err)
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
		EntryID     string   `json:"entry_id"`
		RecordedUSD float64  `json:"recorded_usd"`
		ObservedUSD *float64 `json:"observed_usd"`
		DeltaUSD    *float64 `json:"delta_usd"`
		Exceeds     bool     `json:"exceeds_threshold"`
	}
	var out []flagged
	for _, e := range entries {
		if e.State != cost.StateIncurred && e.State != cost.StateReconciled {
			continue
		}
		obs, ok := observed[e.ID]
		if !ok {
			// No authoritative figure for this entry — report it, but a
			// missing observation is not a variance and is never flagged.
			out = append(out, flagged{EntryID: e.ID, RecordedUSD: e.AmountUSD})
			continue
		}
		v := cost.DetectVariance(e.AmountUSD, obs, *threshold)
		obsCopy, delta := obs, v.DeltaUSD
		out = append(out, flagged{
			EntryID:     e.ID,
			RecordedUSD: e.AmountUSD,
			ObservedUSD: &obsCopy,
			DeltaUSD:    &delta,
			Exceeds:     v.Exceeds,
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"scope": string(scope) + ":" + scopeID, "entries": out})
}

// loadObservedFigures reads a JSON object mapping cost entry id -> authoritative
// provider-reported USD. An empty path returns an empty map (every entry is
// then reported without a variance figure).
func loadObservedFigures(path string) (map[string]float64, error) {
	if path == "" {
		return map[string]float64{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --observed %q: %w", path, err)
	}
	var observed map[string]float64
	if err := json.Unmarshal(raw, &observed); err != nil {
		return nil, fmt.Errorf("parse --observed %q: %w", path, err)
	}
	return observed, nil
}
