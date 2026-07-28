package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.temporal.io/sdk/client"
	"gopkg.in/yaml.v3"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

const missionCmdTimeout = 10 * time.Second

func missionWorkflowID(missionID string) string { return "mission-" + missionID }

func missionLoopName(missionID string) string { return "mission:" + missionID }

func newMissionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mission: generate id: %w", err)
	}
	return "m-" + hex.EncodeToString(buf), nil
}

// runMissionCreate implements `foundry mission create` (docs/PLAN.md
// Task 40 / VEN-01): parses and schema-validates a MissionContract YAML
// file (internal/mission.ParseYAML), persists the mission record, and
// registers its mission-contract.md §3 loop contract -- the row
// MissionLoop's RequireLoopContract activity refuses to start without.
//
// decision (no-gaps rule): this command does not itself start the
// MissionLoop Temporal workflow execution. No CLI command in this package
// starts internal/kernel.DeliverPlan either (`plan submit`/`plan approve`
// persist and classify; execution is driven by whatever orchestrates the
// Temporal worker, outside this task's Scope) -- `mission create` follows
// the same precedent: it makes the mission and its loop contract exist and
// discoverable, and leaves starting the workflow execution to that same
// orchestration path.
func runMissionCreate(args []string) error {
	fs := flag.NewFlagSet("mission create", flag.ContinueOnError)
	principalID := fs.String("principal-id", "", "owning principal id (required)")
	contractPath := fs.String("contract", "", "path to a MissionContract YAML file matching config/schemas/mission.schema.json (required)")
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *principalID == "" || *contractPath == "" {
		return errors.New("mission create: usage: foundry mission create -principal-id=<id> -contract=<path>")
	}

	raw, err := os.ReadFile(*contractPath)
	if err != nil {
		return fmt.Errorf("mission create: read contract %s: %w", *contractPath, err)
	}
	contract, err := mission.ParseYAML(raw)
	if err != nil {
		return fmt.Errorf("mission create: %w", err)
	}

	id, err := newMissionID()
	if err != nil {
		return fmt.Errorf("mission create: %w", err)
	}

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("mission create: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	store := mission.NewStore(db)
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	m := mission.Mission{
		ID:          id,
		PrincipalID: *principalID,
		WorkflowID:  missionWorkflowID(id),
		Contract:    contract,
	}
	if err := store.CreateMission(ctx, m); err != nil {
		return fmt.Errorf("mission create: %w", err)
	}

	budgetJSON, err := json.Marshal(contract.Budget)
	if err != nil {
		return fmt.Errorf("mission create: encode budget: %w", err)
	}
	metricsJSON, err := json.Marshal(struct {
		Metric string `json:"metric"`
	}{contract.Target.Metric})
	if err != nil {
		return fmt.Errorf("mission create: encode metrics: %w", err)
	}
	loopContract := mission.LoopContract{
		LoopName:      missionLoopName(id),
		Trigger:       "mission-active",
		Cadence:       contract.Cadence.Observe,
		Authority:     "mission-loop",
		Budget:        budgetJSON,
		Metrics:       metricsJSON,
		ExitCondition: contract.PostSuccessPolicy,
	}
	if err := store.RegisterLoopContract(ctx, loopContract); err != nil {
		return fmt.Errorf("mission create: %w", err)
	}

	return printMission(m)
}

// runMissionShow implements `foundry mission show <id>`.
func runMissionShow(args []string) error {
	fs := flag.NewFlagSet("mission show", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("mission show: usage: foundry mission show <id>")
	}
	id := fs.Arg(0)

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("mission show: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	store := mission.NewStore(db)
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	m, err := store.GetMission(ctx, id)
	if err != nil {
		return fmt.Errorf("mission show: %w", err)
	}
	return printMission(m)
}

// runMissionPause implements `foundry mission pause <id>`: signals a
// running MissionLoop to pause WAITING/human-command
// (internal/mission.SignalManualPause), independent of any automatic
// pause_when trigger the evaluator itself would raise.
func runMissionPause(args []string) error {
	return signalMission(args, "mission pause", mission.SignalManualPause, func(requestedBy, reason string) any {
		return mission.PauseRequest{RequestedBy: requestedBy, Reason: reason}
	})
}

// runMissionKill implements `foundry mission kill <id>`: signals a running
// MissionLoop to stop cleanly -- CANCELLED/MISSION_KILLED, with a
// product-state handoff note (docs/PLAN.md Task 40 Acceptance).
func runMissionKill(args []string) error {
	return signalMission(args, "mission kill", mission.SignalKillMission, func(requestedBy, reason string) any {
		return mission.KillRequest{RequestedBy: requestedBy, Reason: reason}
	})
}

// runMissionCeremony implements `foundry mission ceremony <id>`:
// checklist walkthrough + MissionReadinessArtifact persistence.
func runMissionCeremony(args []string) error {
	fs := flag.NewFlagSet("mission ceremony", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	approvedBy := fs.String("approved-by", os.Getenv("FOUNDRY_PRINCIPAL"), "principal approving this ceremony")
	checklistPath := fs.String("checklist", "config/ceremony-checklist.yaml", "ceremony checklist yaml")
	answersPath := fs.String("answers", "", "optional YAML answers file for non-interactive runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("mission ceremony: usage: foundry mission ceremony <id> [-approved-by=<principal>] [-answers=<path>]")
	}
	if strings.TrimSpace(*approvedBy) == "" {
		return errors.New("mission ceremony: -approved-by (or FOUNDRY_PRINCIPAL) is required")
	}
	missionID := fs.Arg(0)

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("mission ceremony: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	store := mission.NewStore(db)

	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()
	if _, err := store.GetMission(ctx, missionID); err != nil {
		return fmt.Errorf("mission ceremony: %w", err)
	}

	checklist, err := mission.LoadCeremonyChecklist(*checklistPath)
	if err != nil {
		return err
	}
	answers, err := loadCeremonyAnswers(*answersPath, checklist)
	if err != nil {
		return err
	}
	gates, err := store.ListGateEvents(ctx, missionID)
	if err != nil {
		return fmt.Errorf("mission ceremony: list gate events: %w", err)
	}
	artifact, err := mission.BuildMissionReadinessArtifact(missionID, *approvedBy, checklist, answers, gates, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := store.SaveReadinessArtifact(ctx, artifact); err != nil {
		return fmt.Errorf("mission ceremony: save artifact: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		MissionReadiness mission.MissionReadinessArtifact `json:"mission_readiness"`
	}{MissionReadiness: artifact})
}

func loadCeremonyAnswers(path string, checklist mission.CeremonyChecklist) (map[string]mission.CeremonyAnswer, error) {
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("mission ceremony: read answers %s: %w", path, err)
		}
		var doc struct {
			Answers map[string]mission.CeremonyAnswer `yaml:"answers" json:"answers"`
		}
		if err := json.Unmarshal(raw, &doc); err == nil && len(doc.Answers) > 0 {
			return doc.Answers, nil
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("mission ceremony: decode answers %s: %w", path, err)
		}
		return doc.Answers, nil
	}
	reader := bufio.NewReader(os.Stdin)
	answers := make(map[string]mission.CeremonyAnswer, 16)
	for _, group := range checklist.Groups {
		fmt.Printf("== %s ==\n", group.Name)
		for _, item := range group.Items {
			fmt.Printf("%s [%s] (resolved/deferred): ", item.Prompt, item.Key)
			mode, err := reader.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("mission ceremony: read answer for %s: %w", item.Key, err)
			}
			mode = strings.TrimSpace(strings.ToLower(mode))
			switch mode {
			case "resolved":
				fmt.Print("  evidence: ")
				evidence, err := reader.ReadString('\n')
				if err != nil {
					return nil, fmt.Errorf("mission ceremony: read evidence for %s: %w", item.Key, err)
				}
				answers[item.Key] = mission.CeremonyAnswer{Resolved: true, Evidence: strings.TrimSpace(evidence)}
			case "deferred":
				fmt.Print("  reason: ")
				reason, err := reader.ReadString('\n')
				if err != nil {
					return nil, fmt.Errorf("mission ceremony: read reason for %s: %w", item.Key, err)
				}
				fmt.Print("  revisit_when: ")
				revisit, err := reader.ReadString('\n')
				if err != nil {
					return nil, fmt.Errorf("mission ceremony: read revisit_when for %s: %w", item.Key, err)
				}
				answers[item.Key] = mission.CeremonyAnswer{
					Deferred:    true,
					Reason:      strings.TrimSpace(reason),
					RevisitWhen: strings.TrimSpace(revisit),
				}
			default:
				return nil, fmt.Errorf("mission ceremony: invalid mode for %s: %q (expected resolved|deferred)", item.Key, mode)
			}
		}
	}
	return answers, nil
}

// signalMission is the shared implementation behind runMissionPause and
// runMissionKill: both look up the mission's workflow_id, then send a
// differently-shaped signal payload to it.
func signalMission(args []string, cmdName, signalName string, payload func(requestedBy, reason string) any) error {
	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	requestedBy := fs.String("requested-by", os.Getenv("FOUNDRY_PRINCIPAL"), "principal requesting this action")
	reason := fs.String("reason", "", "reason (required)")
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	temporalHostPort := fs.String("temporal-hostport", os.Getenv("TEMPORAL_HOSTPORT"), "Temporal frontend host:port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%s: usage: foundry %s <id> -reason=<reason> [-requested-by=<principal>]", cmdName, cmdName)
	}
	if *requestedBy == "" {
		return fmt.Errorf("%s: -requested-by (or FOUNDRY_PRINCIPAL) is required", cmdName)
	}
	if *reason == "" {
		return fmt.Errorf("%s: -reason is required", cmdName)
	}
	id := fs.Arg(0)

	sqlDB, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("%s: open postgres: %w", cmdName, err)
	}
	defer func() { _ = sqlDB.Close() }()

	store := mission.NewStore(sqlDB)
	ctx, cancel := context.WithTimeout(context.Background(), missionCmdTimeout)
	defer cancel()

	m, err := store.GetMission(ctx, id)
	if err != nil {
		return fmt.Errorf("%s: %w", cmdName, err)
	}

	if *temporalHostPort == "" {
		*temporalHostPort = "temporal:7233"
	}
	c, err := client.Dial(client.Options{HostPort: *temporalHostPort})
	if err != nil {
		return fmt.Errorf("%s: dial temporal at %s: %w", cmdName, *temporalHostPort, err)
	}
	defer c.Close()

	if err := c.SignalWorkflow(ctx, m.WorkflowID, "", signalName, payload(*requestedBy, *reason)); err != nil {
		return fmt.Errorf("%s: signal workflow %s: %w", cmdName, m.WorkflowID, err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		MissionID  string `json:"mission_id"`
		WorkflowID string `json:"workflow_id"`
		Signal     string `json:"signal"`
	}{m.ID, m.WorkflowID, signalName})
}

func printMission(m mission.Mission) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
