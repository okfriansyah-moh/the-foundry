package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// TestRollout_RealPostgres_LiveLoadLosesZeroUpdates is the real, SQL-driven
// proof of docs/PLAN.md Task 38's Acceptance: "a live rollout during
// running workflows loses zero updates." It runs Rollout concurrently with
// a generator goroutine that keeps appending new transitions to
// workflow_transitions (simulating running workflows), then asserts every
// workflow the generator wrote is present, at its correct final
// (workflow_id, last_seq) pair, in whichever table is workflow_status_projection
// after Rollout returns.
//
// Gated on PROJECTION_TEST_PG_DSN (same convention as
// projector_pg_test.go) since it needs a real Postgres with migrations
// 00002, 00003, and 00011 applied.
func TestRollout_RealPostgres_LiveLoadLosesZeroUpdates(t *testing.T) {
	dsn := os.Getenv("PROJECTION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PROJECTION_TEST_PG_DSN not set — skipping; see projector_pg_test.go's doc comment for setup, plus migrations/00011_projection_versioning.sql")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, stmt := range []string{
		"TRUNCATE workflow_transitions, workflow_status_projection",
		"DELETE FROM projection_offsets",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("reset: %s: %v", stmt, err)
		}
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE "+ShadowTable); err != nil {
		t.Fatalf("reset shadow table: %v", err)
	}

	insert := func(workflowID string, seqInWorkflow int) {
		payload, err := json.Marshal(state.Transition{
			WorkflowID: workflowID,
			Status:     state.StatusRunning,
			PhaseTo:    state.Phase("executing"),
			Attempt:    1,
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO workflow_transitions (workflow_id, payload) VALUES ($1, $2)`,
			workflowID, payload,
		); err != nil {
			t.Fatalf("insert transition %s#%d: %v", workflowID, seqInWorkflow, err)
		}
	}

	// Seed an initial batch and project it, so the live table is
	// non-empty before the "live load" generator and Rollout start
	// racing — the realistic case a rollout runs under.
	const seedWorkflows = 20
	for i := 0; i < seedWorkflows; i++ {
		insert(workflowIDFor(i), 0)
	}
	if _, err := Rebuild(ctx, db, DefaultProjectorName); err != nil {
		t.Fatalf("seed Rebuild: %v", err)
	}

	// Generator: simulates running workflows appending more transitions
	// (both updates to already-seeded workflows and brand-new ones) while
	// Rollout is in flight.
	const generatedWorkflows = 15
	var generatorWG sync.WaitGroup
	generatorWG.Add(1)
	go func() {
		defer generatorWG.Done()
		for i := 0; i < generatedWorkflows; i++ {
			insert(workflowIDFor(i%seedWorkflows), i+1) // update an existing workflow
			insert(newWorkflowIDFor(i), 0)               // and a brand-new one
			time.Sleep(2 * time.Millisecond)
		}
	}()

	result, err := Rollout(ctx, db, "v1")
	generatorWG.Wait()
	if err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if result.ToVersion != "v1" {
		t.Fatalf("result.ToVersion = %q, want %q", result.ToVersion, "v1")
	}

	// The generator may have written transitions AFTER Rollout's last
	// convergence check (a real, documented residual race — see
	// versioning.go's doc comment). Drain the live table one more time
	// with a fresh Projector under DefaultProjectorName to represent the
	// next scheduled `foundry projection rebuild`/tick that would run in
	// production — this is the step that proves "loses zero updates"
	// means "eventually applied," not "zero-lag at the exact swap
	// instant."
	live := &Projector{DB: db}
	for {
		n, err := live.Tick(ctx)
		if err != nil {
			t.Fatalf("post-swap drain Tick: %v", err)
		}
		if n == 0 {
			break
		}
	}

	var expectedWorkflows int64
	if err := db.QueryRowContext(ctx, `SELECT count(DISTINCT workflow_id) FROM workflow_transitions`).Scan(&expectedWorkflows); err != nil {
		t.Fatalf("count distinct workflows: %v", err)
	}
	var projectedWorkflows int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workflow_status_projection`).Scan(&projectedWorkflows); err != nil {
		t.Fatalf("count projected rows: %v", err)
	}
	if projectedWorkflows != expectedWorkflows {
		t.Fatalf("projected rows = %d, want %d (a live update was lost)", projectedWorkflows, expectedWorkflows)
	}

	// Every workflow's projected last_seq must equal its true latest seq
	// in workflow_transitions -- the concrete "loses zero updates" check.
	rows, err := db.QueryContext(ctx, `
		SELECT p.workflow_id, p.last_seq, t.max_seq
		FROM workflow_status_projection p
		JOIN (SELECT workflow_id, max(seq) AS max_seq FROM workflow_transitions GROUP BY workflow_id) t
		  ON t.workflow_id = p.workflow_id
		WHERE p.last_seq <> t.max_seq`)
	if err != nil {
		t.Fatalf("query mismatches: %v", err)
	}
	defer rows.Close()
	var mismatches int
	for rows.Next() {
		var wf string
		var got, want int64
		if err := rows.Scan(&wf, &got, &want); err != nil {
			t.Fatalf("scan mismatch row: %v", err)
		}
		t.Errorf("workflow %s projected last_seq=%d, want %d (latest transition)", wf, got, want)
		mismatches++
	}
	if mismatches > 0 {
		t.Fatalf("%d workflow(s) did not reflect their latest transition after rollout + drain", mismatches)
	}
}

func workflowIDFor(i int) string    { return "rollout-seed-wf-" + strconv.Itoa(i) }
func newWorkflowIDFor(i int) string { return "rollout-gen-wf-" + strconv.Itoa(i) }
