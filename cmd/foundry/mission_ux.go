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
	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// docs/PLAN.md Task 107 (RTC-03): mission operational UX — start, resume, list
// and a richer status read. These are read/transport commands; the kernel owns
// the workflow (Constitution C4).

func openMissionStore(pgDSN string) (*sql.DB, *mission.Store, error) {
	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("mission: open postgres: %w", err)
	}
	return db, mission.NewStore(db), nil
}

func runMissionList(args []string) error {
	fs := flag.NewFlagSet("mission list", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	status := fs.String("status", "", "filter by latest mission status")
	profile := fs.String("profile", "", "filter by profile (reserved; enforced by Task 118)")
	limit := fs.Int("limit", 50, "max rows")
	offset := fs.Int("offset", 0, "row offset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db, store, err := openMissionStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	items, err := store.ListMissions(ctx, mission.MissionFilter{
		Status: *status, Profile: *profile, Limit: *limit, Offset: *offset,
	})
	if err != nil {
		return fmt.Errorf("mission list: %w", err)
	}
	fmt.Printf("%-28s  %-12s  %-20s  %s\n", "ID", "STATUS", "REASON", "WORKFLOW")
	for _, it := range items {
		st := it.Status
		if st == "" {
			st = "(no state)"
		}
		fmt.Printf("%-28s  %-12s  %-20s  %s\n", it.ID, st, it.Reason, it.WorkflowID)
	}
	return nil
}

func runMissionStatus(args []string) error {
	fs := flag.NewFlagSet("mission status", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("mission status: usage: foundry mission status <id>")
	}
	id := fs.Arg(0)

	db, store, err := openMissionStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	m, err := store.GetMission(ctx, id)
	if err != nil {
		return fmt.Errorf("mission status: %w", err)
	}
	fmt.Printf("Mission %s\n  workflow: %s\n  statement: %s\n  monthly budget: $%.2f\n  experiment budget: $%.2f\n",
		m.ID, m.WorkflowID, m.Contract.Statement, m.Contract.Budget.MonthlyUSD, m.Contract.Budget.TotalExperimentUSD)

	state, err := store.LatestState(ctx, id)
	if err != nil {
		if errors.Is(err, mission.ErrNotFound) {
			fmt.Println("  loop state: none recorded yet")
			return nil
		}
		return fmt.Errorf("mission status: %w", err)
	}
	fmt.Printf("  status: %s (%s)\n  cycle: %d  net_mrr: $%.2f  no_progress: %d\n  last observed: %s\n",
		state.Status, state.Reason, state.Cycle, state.NetMRRUSD, state.NoProgressCycles, state.ObservedAt.UTC().Format(time.RFC3339))
	return nil
}

func runMissionResume(args []string) error {
	fs := flag.NewFlagSet("mission resume", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	temporalHostPort := fs.String("temporal-hostport", os.Getenv("TEMPORAL_HOSTPORT"), "Temporal frontend host:port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("mission resume: usage: foundry mission resume <id>")
	}
	id := fs.Arg(0)

	db, store, err := openMissionStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	m, err := store.GetMission(ctx, id)
	if err != nil {
		return fmt.Errorf("mission resume: %w", err)
	}
	// Refuse to resume a mission that is not currently WAITING.
	if st, err := store.LatestState(ctx, id); err == nil {
		if st.Status != "WAITING" {
			return fmt.Errorf("mission resume: mission %s is %s, not WAITING — nothing to resume", id, st.Status)
		}
	}

	if *temporalHostPort == "" {
		*temporalHostPort = "temporal:7233"
	}
	c, err := client.Dial(client.Options{HostPort: *temporalHostPort})
	if err != nil {
		return fmt.Errorf("mission resume: dial temporal: %w", err)
	}
	defer c.Close()
	if err := c.SignalWorkflow(ctx, m.WorkflowID, "", mission.SignalResumeMission, nil); err != nil {
		return fmt.Errorf("mission resume: signal %s: %w", m.WorkflowID, err)
	}
	fmt.Printf("resumed mission %s (workflow %s)\n", id, m.WorkflowID)
	return nil
}

func runMissionStart(args []string) error {
	fs := flag.NewFlagSet("mission start", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	temporalHostPort := fs.String("temporal-hostport", os.Getenv("TEMPORAL_HOSTPORT"), "Temporal frontend host:port")
	queueConfig := fs.String("queue-config", "config/queue-priority.yaml", "queue-priority config path")
	lane := fs.String("lane", "", "lane to run the mission loop on (empty => delivery default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("mission start: usage: foundry mission start <id>")
	}
	id := fs.Arg(0)

	db, store, err := openMissionStore(*pgDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	m, err := store.GetMission(ctx, id)
	if err != nil {
		return fmt.Errorf("mission start: %w", err)
	}

	queueCfg, err := observe.LoadQueueConfig(*queueConfig)
	if err != nil {
		return fmt.Errorf("mission start: load queue config: %w", err)
	}
	taskQueue, err := kernel.LaneSelector{}.Select(*lane, queueCfg)
	if err != nil {
		return fmt.Errorf("mission start: resolve lane: %w", err)
	}

	if *temporalHostPort == "" {
		*temporalHostPort = "temporal:7233"
	}
	c, err := client.Dial(client.Options{HostPort: *temporalHostPort})
	if err != nil {
		return fmt.Errorf("mission start: dial temporal: %w", err)
	}
	defer c.Close()

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        m.WorkflowID,
		TaskQueue: taskQueue,
	}, mission.MissionLoop, mission.MissionLoopInput{
		MissionID:         m.ID,
		Contract:          m.Contract,
		DeliveryTaskQueue: taskQueue,
	})
	if err != nil {
		return fmt.Errorf("mission start: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		MissionID  string `json:"mission_id"`
		WorkflowID string `json:"workflow_id"`
		RunID      string `json:"run_id"`
		TaskQueue  string `json:"task_queue"`
	}{m.ID, m.WorkflowID, run.GetRunID(), taskQueue})
}
