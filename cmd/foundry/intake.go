package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
)

// docs/PLAN.md Task 111 (INT-03): the `foundry mission start --idea` intake
// pipeline and its `foundry intake show|resume|list` read/resume surface. This
// command is orchestration only — it makes no authority decision, never sets a
// declared tier, and never approves a plan it generated (Constitution C6).

const intakeCmdTimeout = 60 * time.Second

// intakeFlags collects the flags shared by `mission start --idea` and any
// future intake entry point.
type intakeFlags struct {
	idea               string
	budget             float64
	dryRun             bool
	pgDSN              string
	specCassette       string
	opportunityFixture string
	opportunityConfig  string
	repoURL            string
	repoAlias          string
	repoBranch         string
	repoWriteTarget    string
	temporalHostPort   string
}

// runIntakeStart implements `foundry mission start --idea "<text>" --budget N`.
// It builds the pipeline from cassette-backed, zero-network seams by default
// (the same offline path the e2e uses) and drives the run to a terminal or a
// pause, printing every stage transition.
func runIntakeStart(args []string) error {
	fs := flag.NewFlagSet("mission start --idea", flag.ContinueOnError)
	f := &intakeFlags{}
	fs.StringVar(&f.idea, "idea", "", "raw idea text (required for the intake path)")
	fs.Float64Var(&f.budget, "budget", 0, "mission budget envelope in USD (establishes the envelope before any spend)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "run entirely in memory with a recording mission starter (no Postgres, no Temporal)")
	fs.StringVar(&f.pgDSN, "pg-dsn", pgDSNFromEnv(), "Postgres DSN (persistent, resumable runs)")
	fs.StringVar(&f.specCassette, "spec-cassette", "", "spec ReplaySource cassette (offline synthesis)")
	fs.StringVar(&f.opportunityFixture, "opportunity-fixtures", "", "directory of opportunity fixtures keyed by idea digest (offline validation)")
	fs.StringVar(&f.opportunityConfig, "opportunity-config", "config/opportunity-thresholds.yaml", "opportunity scoring config")
	fs.StringVar(&f.repoURL, "repo-url", "", "mission repository URL (required)")
	fs.StringVar(&f.repoAlias, "repo-alias", "product", "mission repository alias")
	fs.StringVar(&f.repoBranch, "repo-branch", "main", "mission repository branch")
	fs.StringVar(&f.repoWriteTarget, "repo-write-target", "", "least-privilege repo-write path (required, never '*')")
	fs.StringVar(&f.temporalHostPort, "temporal-hostport", os.Getenv("TEMPORAL_HOSTPORT"), "Temporal frontend host:port (live mission start)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.idea == "" {
		return errors.New("mission start --idea: idea text is required")
	}
	if f.repoURL == "" || f.repoWriteTarget == "" {
		return errors.New("mission start --idea: --repo-url and --repo-write-target are required (no literal fallback, never '*')")
	}

	ctx, cancel := context.WithTimeout(context.Background(), intakeCmdTimeout)
	defer cancel()

	// Store: persistent (Postgres) unless a dry run is requested.
	var store intake.Store
	var db *sql.DB
	if f.dryRun {
		store = intake.NewMemStore()
	} else {
		var err error
		db, err = sql.Open("pgx", f.pgDSN)
		if err != nil {
			return fmt.Errorf("mission start --idea: open db: %w", err)
		}
		defer func() { _ = db.Close() }()
		store = intake.NewPGStore(db)
	}

	deps, err := buildIntakeDeps(store, f, db)
	if err != nil {
		return err
	}
	p, err := intake.NewPipeline(deps)
	if err != nil {
		return err
	}
	run, err := p.Start(ctx, intake.StartInput{
		Idea:   f.idea,
		Budget: f.budget,
		Origin: intake.Origin{Channel: "cli"},
	})
	if err != nil {
		return fmt.Errorf("mission start --idea: %w", err)
	}
	printIntakeRun(run)
	return nil
}

// printIntakeRun renders a run's terminal or paused state, including the
// operator's next actions for a terminal-by-design outcome (Task 144 output
// contract: run/opportunity/verdict/spec/plan/tier/approval/mission/status).
func printIntakeRun(run intake.Run) {
	fmt.Printf("intake run %s\n  stage:  %s\n  status: %s\n  spent:  $%.4f of $%.2f envelope\n",
		run.ID, run.CurrentStage, run.Status, run.SpentUSD, run.Caps.EnvelopeUSD)
	if run.MissionID != "" {
		fmt.Printf("  mission: %s\n", run.MissionID)
	}
	switch run.CurrentStage {
	case intake.StageMissionStarted:
		fmt.Printf("  workflow: missionloop-%s\n", run.MissionID)
		fmt.Println("  outcome: MissionLoop started (production Temporal path)")
	case intake.StageOpportunityRejected, intake.StageOpportunityValidationRequired:
		fmt.Println("  outcome: build nothing (this is a successful terminal outcome)")
	case intake.StageAwaitingStrongAuth:
		fmt.Println("  outcome: halted awaiting strong-auth approval — this plan will not auto-approve")
	case intake.StageAwaitingReadiness:
		fmt.Println("  outcome: halted awaiting mission-readiness ceremony answers")
	}
}

// runIntakeShow implements `foundry intake show <id>`.
func runIntakeShow(args []string) error {
	fs := flag.NewFlagSet("intake show", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("intake show: usage: foundry intake show <id>")
	}
	db, store, cancel, err := openIntakeStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, done := cancel()
	defer done()
	run, err := store.GetRun(ctx, fs.Arg(0))
	if err != nil {
		return fmt.Errorf("intake show: %w", err)
	}
	out, _ := json.MarshalIndent(run, "", "  ")
	fmt.Println(string(out))
	return nil
}

// runIntakeResume implements `foundry intake resume <id>` — it advances a paused
// or interrupted run from where it stopped, without duplicating any prior stage.
func runIntakeResume(args []string) error {
	fs := flag.NewFlagSet("intake resume", flag.ContinueOnError)
	f := &intakeFlags{}
	fs.StringVar(&f.pgDSN, "pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	fs.StringVar(&f.specCassette, "spec-cassette", "", "spec ReplaySource cassette")
	fs.StringVar(&f.opportunityFixture, "opportunity-fixtures", "", "opportunity fixtures directory")
	fs.StringVar(&f.opportunityConfig, "opportunity-config", "config/opportunity-thresholds.yaml", "opportunity scoring config")
	fs.StringVar(&f.repoURL, "repo-url", "", "mission repository URL")
	fs.StringVar(&f.repoAlias, "repo-alias", "product", "mission repository alias")
	fs.StringVar(&f.repoBranch, "repo-branch", "main", "mission repository branch")
	fs.StringVar(&f.repoWriteTarget, "repo-write-target", "", "least-privilege repo-write path")
	fs.StringVar(&f.temporalHostPort, "temporal-hostport", os.Getenv("TEMPORAL_HOSTPORT"), "Temporal frontend host:port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("intake resume: usage: foundry intake resume <id>")
	}
	db, err := sql.Open("pgx", f.pgDSN)
	if err != nil {
		return fmt.Errorf("intake resume: open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	store := intake.NewPGStore(db)
	deps, err := buildIntakeDeps(store, f, db)
	if err != nil {
		return err
	}
	p, err := intake.NewPipeline(deps)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), intakeCmdTimeout)
	defer cancel()
	run, err := p.Resume(ctx, fs.Arg(0))
	if err != nil {
		return fmt.Errorf("intake resume: %w", err)
	}
	printIntakeRun(run)
	return nil
}

// runIntakeList implements `foundry intake list`.
func runIntakeList(args []string) error {
	fs := flag.NewFlagSet("intake list", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	limit := fs.Int("limit", 50, "max rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db, store, cancel, err := openIntakeStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, done := cancel()
	defer done()
	runs, err := store.ListRuns(ctx, *limit)
	if err != nil {
		return fmt.Errorf("intake list: %w", err)
	}
	for _, r := range runs {
		fmt.Printf("%s  %-32s  %-10s  $%.2f  %s\n", r.ID, r.CurrentStage, r.Status, r.Caps.EnvelopeUSD, truncate(r.Idea, 48))
	}
	return nil
}

func openIntakeStore(pgDSN string) (*sql.DB, *intake.PGStore, func() (context.Context, context.CancelFunc), error) {
	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("intake: open db: %w", err)
	}
	cancel := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), intakeCmdTimeout)
	}
	return db, intake.NewPGStore(db), cancel, nil
}
