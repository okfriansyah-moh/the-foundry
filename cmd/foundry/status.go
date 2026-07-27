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
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

const statusCmdTimeout = 30 * time.Second

// projectionRow is a decoded workflow_status_projection row — the projected
// read path's data (Task 14 / internal/db/migrations/00003_projection.sql).
type projectionRow struct {
	WorkflowID string
	Status     string
	Phase      string
	UpdatedAt  time.Time
	LastSeq    int64
}

// statusArgs is the parsed form of `foundry status <workflow-id> [--fresh]`.
// The workflow ID is positional and may appear before or after flags, so
// parsing is split out from flag.FlagSet (which stops at the first
// non-flag token) and is unit-testable without a live server.
type statusArgs struct {
	workflowID       string
	fresh            bool
	pgDSN            string
	temporalHostPort string
	temporalNS       string
	// apiAddr, when set, routes this command over foundryd's HTTP API
	// (docs/PLAN.md Task 36: "CLI reimplemented over API for status+
	// submit paths") instead of querying Postgres/Temporal directly. It
	// is opt-in (empty by default) so every existing direct-DB caller —
	// test/status_consistency_e2e.sh, test/skp_e2e.sh,
	// test/skp_resume_test.sh, none of which set --api-addr — keeps its
	// current behavior unchanged.
	apiAddr string
}

func parseStatusArgs(args []string) (statusArgs, error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fresh := fs.Bool("fresh", false, "read through to Temporal instead of the PG projection")
	pgDSN := fs.String("pg-dsn", "", "Postgres DSN (defaults to $PG_DSN)")
	temporalHostPort := fs.String("temporal-hostport", "", "Temporal frontend host:port (defaults to $TEMPORAL_HOSTPORT)")
	temporalNS := fs.String("temporal-namespace", "", "Temporal namespace (defaults to $TEMPORAL_NAMESPACE, then \"default\")")
	apiAddr := fs.String("api-addr", os.Getenv("FOUNDRY_API_ADDR"), "foundryd API base URL (e.g. http://localhost:8080); when set, status is read over the API instead of Postgres/Temporal directly")

	var workflowID string
	var flagArgs []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			continue
		}
		if workflowID == "" {
			workflowID = a
			continue
		}
		flagArgs = append(flagArgs, a)
	}

	if err := fs.Parse(flagArgs); err != nil {
		return statusArgs{}, err
	}
	if workflowID == "" {
		return statusArgs{}, errors.New("usage: foundry status <workflow-id> [--fresh]")
	}

	dsn := *pgDSN
	if dsn == "" {
		dsn = os.Getenv("PG_DSN")
	}
	if dsn == "" {
		dsn = "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable"
	}

	hostPort := *temporalHostPort
	if hostPort == "" {
		hostPort = os.Getenv("TEMPORAL_HOSTPORT")
	}
	if hostPort == "" {
		hostPort = "temporal:7233"
	}

	ns := *temporalNS
	if ns == "" {
		ns = os.Getenv("TEMPORAL_NAMESPACE")
	}
	if ns == "" {
		ns = "default"
	}

	return statusArgs{
		workflowID:       workflowID,
		fresh:            *fresh,
		pgDSN:            dsn,
		temporalHostPort: hostPort,
		temporalNS:       ns,
		apiAddr:          *apiAddr,
	}, nil
}

// projectionLag returns how stale a projection row is (now - updatedAt),
// floored at zero — clock skew between the CLI host and Postgres must never
// print a negative lag (data-consistency.md §2: "projection lag is a
// first-class metric").
func projectionLag(updatedAt, now time.Time) time.Duration {
	d := now.Sub(updatedAt)
	if d < 0 {
		return 0
	}
	return d
}

// formatProjected renders the projected-read-path output (docs/PLAN.md Task
// 15 Step: "prints consistency: projected (lag: Xs)").
func formatProjected(row projectionRow, lag time.Duration) string {
	return fmt.Sprintf(
		"workflow_id: %s\nstatus: %s\nphase: %s\nlast_seq: %d\nconsistency: projected (lag: %.0fs)\n",
		row.WorkflowID, row.Status, row.Phase, row.LastSeq, lag.Seconds(),
	)
}

// formatFresh renders the --fresh output: Temporal's own execution status
// plus the last transition read directly off workflow_transitions, bypassing
// the projection entirely (docs/PLAN.md Task 15 Step: "prints consistency:
// fresh").
func formatFresh(workflowID string, temporalStatus string, last state.Transition) string {
	return fmt.Sprintf(
		"workflow_id: %s\nstatus: %s\nphase: %s\ntemporal_status: %s\nconsistency: fresh\n",
		workflowID, last.Status, last.PhaseTo, temporalStatus,
	)
}

// runStatus implements `foundry status <workflow-id> [--fresh]` (docs/PLAN.md
// Task 15 / SKP-13). The projected path reads workflow_status_projection
// (Task 14); --fresh reads Temporal's DescribeWorkflowExecution plus the
// latest workflow_transitions row directly — no projection table read on
// this path (data-consistency.md §2: stale-read labeling).
func runStatus(args []string) error {
	parsed, err := parseStatusArgs(args)
	if err != nil {
		return err
	}

	// docs/PLAN.md Task 36 dogfood: --api-addr (or $FOUNDRY_API_ADDR)
	// routes this command over foundryd's HTTP API instead of querying
	// Postgres/Temporal directly. Opt-in and additive — see statusArgs.apiAddr.
	if parsed.apiAddr != "" {
		return runStatusViaAPI(parsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusCmdTimeout)
	defer cancel()

	db, err := sql.Open("pgx", parsed.pgDSN)
	if err != nil {
		return fmt.Errorf("status: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if parsed.fresh {
		return runStatusFresh(ctx, db, parsed)
	}
	return runStatusProjected(ctx, db, parsed)
}

func runStatusProjected(ctx context.Context, db *sql.DB, parsed statusArgs) error {
	const q = `
SELECT workflow_id, status, phase, last_seq, updated_at
FROM workflow_status_projection
WHERE workflow_id = $1`

	var row projectionRow
	err := db.QueryRowContext(ctx, q, parsed.workflowID).Scan(
		&row.WorkflowID, &row.Status, &row.Phase, &row.LastSeq, &row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("status: no projection row for workflow %s", parsed.workflowID)
	}
	if err != nil {
		return fmt.Errorf("status: query projection: %w", err)
	}

	lag := projectionLag(row.UpdatedAt, time.Now())
	fmt.Print(formatProjected(row, lag))
	return nil
}

func runStatusFresh(ctx context.Context, db *sql.DB, parsed statusArgs) error {
	last, err := queryLastTransition(ctx, db, parsed.workflowID)
	if err != nil {
		return fmt.Errorf("status --fresh: query last transition: %w", err)
	}

	temporalStatus, err := describeTemporalWorkflow(ctx, parsed.temporalHostPort, parsed.temporalNS, parsed.workflowID)
	if err != nil {
		return fmt.Errorf("status --fresh: describe temporal workflow: %w", err)
	}

	fmt.Print(formatFresh(parsed.workflowID, temporalStatus, last))
	return nil
}

// queryLastTransition reads the single latest workflow_transitions row for
// workflowID directly — deliberately not workflow_status_projection, so the
// --fresh label stays honest.
func queryLastTransition(ctx context.Context, db *sql.DB, workflowID string) (state.Transition, error) {
	const q = `
SELECT payload
FROM workflow_transitions
WHERE workflow_id = $1
ORDER BY seq DESC
LIMIT 1`

	var payload []byte
	err := db.QueryRowContext(ctx, q, workflowID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return state.Transition{}, fmt.Errorf("no transitions recorded for workflow %s", workflowID)
	}
	if err != nil {
		return state.Transition{}, err
	}

	var t state.Transition
	if err := json.Unmarshal(payload, &t); err != nil {
		return state.Transition{}, fmt.Errorf("decode transition: %w", err)
	}
	return t, nil
}

// describeTemporalWorkflow calls Temporal's DescribeWorkflowExecution over
// the same raw workflowservice gRPC client runDoctor uses (cmd/foundry/doctor.go)
// and returns the workflow execution status as a string.
func describeTemporalWorkflow(ctx context.Context, hostPort, namespace, workflowID string) (string, error) {
	conn, err := grpc.NewClient(hostPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("dial temporal: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client := workflowservice.NewWorkflowServiceClient(conn)
	resp, err := client.DescribeWorkflowExecution(ctx, &workflowservice.DescribeWorkflowExecutionRequest{
		Namespace: namespace,
		Execution: &commonpb.WorkflowExecution{WorkflowId: workflowID},
	})
	if err != nil {
		return "", fmt.Errorf("DescribeWorkflowExecution: %w", err)
	}
	return resp.GetWorkflowExecutionInfo().GetStatus().String(), nil
}
