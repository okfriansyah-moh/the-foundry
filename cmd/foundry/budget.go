package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

const budgetCmdTimeout = 10 * time.Second

// runBudgetRaise implements `foundry budget raise` (docs/PLAN.md
// Task 29/FND-10, Constitution C19): raises one budget envelope's
// ceiling — an audited operation (internal/provenance's AppendAuditRow,
// the same minimal hash-chain writer Task 24's `plan revoke` uses, per
// its own doc comment on why no separate audit package exists yet) — and,
// if --workflow-id is given, signals that workflow's paused DeliverPlan
// run (internal/kernel.SignalBudgetRaised) so it resumes without a
// restart, which is this task's Acceptance bar: "resumable after `foundry
// budget raise`".
func runBudgetRaise(args []string) error {
	fs := flag.NewFlagSet("budget raise", flag.ContinueOnError)
	scopeArg := fs.String("scope", "", "envelope scope, as \"<scope>:<id>\" e.g. \"mission:m1\" (required)")
	kindArg := fs.String("kind", "", "envelope kind: mission_monthly|provider|infra|experiment|reserve (required)")
	period := fs.String("period", "", "envelope period, e.g. \"2026-07\" (required)")
	ceilingArg := fs.String("ceiling", "", "new ceiling in USD, must exceed the current ceiling (required)")
	raisedBy := fs.String("raised-by", os.Getenv("FOUNDRY_PRINCIPAL"), "principal performing the raise")
	reason := fs.String("reason", "", "reason for raising the ceiling (required)")
	workflowID := fs.String("workflow-id", "", "if set, signal this paused workflow to resume after raising the ceiling")
	pgDSN := fs.String("pg-dsn", os.Getenv("PG_DSN"), "Postgres DSN")
	temporalHostPort := fs.String("temporal-hostport", os.Getenv("TEMPORAL_HOSTPORT"), "Temporal frontend host:port (only used with --workflow-id)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *scopeArg == "" || *kindArg == "" || *period == "" || *ceilingArg == "" {
		return errors.New("budget raise: --scope, --kind, --period, and --ceiling are all required")
	}
	if *raisedBy == "" {
		return errors.New("budget raise: --raised-by (or FOUNDRY_PRINCIPAL) is required")
	}
	if *reason == "" {
		return errors.New("budget raise: --reason is required")
	}
	scope, scopeID, err := parseScopeArg(*scopeArg)
	if err != nil {
		return fmt.Errorf("budget raise: %w", err)
	}
	kind := cost.Kind(*kindArg)
	switch kind {
	case cost.KindMissionMonthly, cost.KindProvider, cost.KindInfra, cost.KindExperiment, cost.KindReserve:
	default:
		return fmt.Errorf("budget raise: invalid --kind %q", *kindArg)
	}
	ceilingUSD, err := strconv.ParseFloat(*ceilingArg, 64)
	if err != nil {
		return fmt.Errorf("budget raise: invalid --ceiling %q: %w", *ceilingArg, err)
	}
	if *pgDSN == "" {
		return errors.New("budget raise: no --pg-dsn/PG_DSN set")
	}

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("budget raise: open postgres: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), budgetCmdTimeout)
	defer cancel()

	store := cost.NewStore(db)
	raised, err := store.RaiseCeiling(ctx, scope, scopeID, kind, *period, ceilingUSD)
	if err != nil {
		return fmt.Errorf("budget raise: %w", err)
	}

	payload, err := json.Marshal(struct {
		Scope      string  `json:"scope"`
		ScopeID    string  `json:"scope_id"`
		Kind       string  `json:"kind"`
		Period     string  `json:"period"`
		CeilingUSD float64 `json:"ceiling_usd"`
		RaisedBy   string  `json:"raised_by"`
		Reason     string  `json:"reason"`
	}{string(scope), scopeID, string(kind), *period, ceilingUSD, *raisedBy, *reason})
	if err != nil {
		return fmt.Errorf("budget raise: marshal audit payload: %w", err)
	}
	subject := fmt.Sprintf("%s:%s:%s:%s", scope, scopeID, kind, *period)
	if err := provenance.AppendAuditRow(ctx, db, *raisedBy, "budget.raise", subject, payload); err != nil {
		return fmt.Errorf("budget raise: %w", err)
	}

	if *workflowID != "" {
		if *temporalHostPort == "" {
			*temporalHostPort = "temporal:7233"
		}
		c, err := client.Dial(client.Options{HostPort: *temporalHostPort})
		if err != nil {
			return fmt.Errorf("budget raise: dial temporal at %s: %w", *temporalHostPort, err)
		}
		defer c.Close()
		if err := c.SignalWorkflow(ctx, *workflowID, "", kernel.SignalBudgetRaised, nil); err != nil {
			return fmt.Errorf("budget raise: signal workflow %s: %w", *workflowID, err)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(raised)
}
