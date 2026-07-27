package extops_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// harnessEnvVar re-execs this test binary as a standalone "activity
// process" instead of running the normal test suite — the standard Go
// subprocess-test-helper pattern (as used by os/exec's own tests), chosen
// here specifically so the crash-injection test below can SIGKILL a real
// OS process talking to a real Postgres, not a goroutine sharing this
// test binary's own memory. A same-process panic/recover would prove
// nothing about what happens to an in-flight transaction when the
// process actually dies (see this file's package doc comment below).
const harnessEnvVar = "EXTOPS_CRASH_HARNESS"

// TestMain intercepts harness re-exec before any normal test runs.
func TestMain(m *testing.M) {
	if os.Getenv(harnessEnvVar) == "1" {
		runCrashHarness()
		return
	}
	os.Exit(m.Run())
}

// runCrashHarness plays the role of a real kernel activity performing one
// external side effect: it reserves the operation, performs the side
// effect for real (an INSERT against a side-effect table, committed on
// its own), marks the operation executed (a second, separately committed
// UPDATE), announces that fact on stdout, and then blocks forever —
// waiting to be SIGKILLed by the parent test at a moment the parent
// controls precisely, rather than exiting cleanly on its own. Config
// arrives via env vars so the parent can drive it via exec.Command.
func runCrashHarness() {
	dsn := os.Getenv("EXTOPS_HARNESS_DSN")
	key := os.Getenv("EXTOPS_HARNESS_KEY")
	table := os.Getenv("EXTOPS_HARNESS_TABLE")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Println("HARNESS_ERROR open:", err)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		fmt.Println("HARNESS_ERROR ping:", err)
		os.Exit(1)
	}

	store := extops.NewStore(db)
	op, err := store.Reserve(ctx, "wf-crash", "scm.push", "org/repo#main", key, map[string]string{"sha": "deadbeef"})
	if err != nil {
		fmt.Println("HARNESS_ERROR reserve:", err)
		os.Exit(1)
	}

	// The real, irreversible side effect: a durably committed row proving
	// "the push already happened", exactly like a real git push landing
	// on the remote before our own bookkeeping catches up.
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (idempotency_key) VALUES ($1)`, table), key); err != nil {
		fmt.Println("HARNESS_ERROR side effect insert:", err)
		os.Exit(1)
	}

	receipt := map[string]string{"sha": "deadbeef", "op_id": string(op.ID)}
	if _, err := store.MarkExecuted(ctx, op.ID, receipt); err != nil {
		fmt.Println("HARNESS_ERROR mark executed:", err)
		os.Exit(1)
	}

	// The ledger write is now durably committed in Postgres. Announce it,
	// then hang — the parent SIGKILLs us the instant it reads this line,
	// so the kill lands strictly after commit and strictly before this
	// process could do anything else (e.g. ack back to a Temporal
	// worker), which is exactly the crash window a lost activity-ack
	// represents.
	fmt.Println("COMMITTED")
	os.Stdout.Sync()
	select {}
}

func uniqueHarnessKey(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return "crash-" + hex.EncodeToString(buf)
}

// TestCrashInjection_ReplayAfterKillReturnsReceiptWithoutRerunningFn is
// the task's load-bearing proof (docs/PLAN.md Task 26 Acceptance): a real
// separate OS process performs a real external side effect against a real
// Postgres, durably commits the ledger's "executed" record of it, and is
// then SIGKILLed — a genuine, unrecoverable process death, not a
// same-process panic/recover, which would only prove Go's exception
// handling and say nothing about surviving DB state after a real crash.
//
// After the kill, this test process calls the production
// kernel.WithExternalOp function (not a reimplementation of its logic)
// with the identical idempotency key and a second fn that would insert
// another side-effect row if invoked. It asserts: fn is never invoked,
// the returned receipt matches what the killed process recorded, and the
// side-effect table still has exactly one row for this key — i.e. the
// replay took the "already executed, return receipt" path instead of
// repeating the side effect.
func TestCrashInjection_ReplayAfterKillReturnsReceiptWithoutRerunningFn(t *testing.T) {
	dsn := testDSN()
	if dsn == "" {
		t.Skip("EXTOPS_TEST_PG_DSN/PG_DSN not set — skipping; run via `docker compose run --rm dev go test ./internal/ledger/... -run CrashInjection` for a real Postgres")
	}

	db := openTestDB(t)
	const sideEffectTable = "extops_crash_side_effects"
	if _, err := db.ExecContext(context.Background(), fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (idempotency_key TEXT NOT NULL, id SERIAL PRIMARY KEY)`, sideEffectTable,
	)); err != nil {
		t.Fatalf("create side-effect table: %v", err)
	}

	key := uniqueHarnessKey(t)

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("find test binary: %v", err)
	}

	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(),
		harnessEnvVar+"=1",
		"EXTOPS_HARNESS_DSN="+dsn,
		"EXTOPS_HARNESS_KEY="+key,
		"EXTOPS_HARNESS_TABLE="+sideEffectTable,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start harness process: %v", err)
	}

	// Watch for the harness's COMMITTED announcement, with a hard
	// deadline so a broken harness fails the test instead of hanging CI.
	lineCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "COMMITTED" || strings.HasPrefix(line, "HARNESS_ERROR") {
				lineCh <- line
				return
			}
		}
		lineCh <- ""
	}()

	select {
	case line := <-lineCh:
		if line != "COMMITTED" {
			_ = cmd.Process.Kill()
			t.Fatalf("harness did not report COMMITTED, got: %q", line)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("timed out waiting for harness to commit")
	}

	// The real crash: an unconditional, immediate SIGKILL — the harness
	// gets no chance to run any further code, deferred or otherwise.
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL harness process: %v", err)
	}
	waitErr := cmd.Wait()
	if waitErr == nil {
		t.Fatal("harness process exited cleanly instead of being killed — the crash was not real")
	}

	// Confirm the side effect really did land exactly once before
	// replaying — the ledger's own state is confirmed below via
	// WithExternalOp itself, exactly the path a real replay takes.
	store := extops.NewStore(db)

	var preCount int
	if err := db.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE idempotency_key = $1`, sideEffectTable), key,
	).Scan(&preCount); err != nil {
		t.Fatalf("count side effects pre-replay: %v", err)
	}
	if preCount != 1 {
		t.Fatalf("side-effect rows before replay = %d, want exactly 1 (the killed process's own insert)", preCount)
	}

	var fnCalls int32
	fn := func(context.Context) (map[string]string, error) {
		atomic.AddInt32(&fnCalls, 1)
		if _, err := db.ExecContext(context.Background(),
			fmt.Sprintf(`INSERT INTO %s (idempotency_key) VALUES ($1)`, sideEffectTable), key,
		); err != nil {
			return nil, err
		}
		return map[string]string{"sha": "deadbeef-DUPLICATE"}, nil
	}

	receipt, err := kernel.WithExternalOp(context.Background(), store, "wf-crash", "scm.push", "org/repo#main", key, map[string]string{"sha": "deadbeef"}, fn)
	if err != nil {
		t.Fatalf("WithExternalOp replay: %v", err)
	}

	if atomic.LoadInt32(&fnCalls) != 0 {
		t.Fatalf("fn was invoked %d time(s) on replay — the killed process's side effect was repeated", fnCalls)
	}

	want := map[string]string{"sha": "deadbeef", "op_id": receipt["op_id"]}
	if !reflect.DeepEqual(receipt, want) {
		t.Fatalf("replay receipt = %+v, want %+v (the record the killed process wrote, not a fresh one)", receipt, want)
	}
	if receipt["sha"] == "deadbeef-DUPLICATE" {
		t.Fatal("replay returned the duplicate-run receipt, not the original")
	}

	var postCount int
	if err := db.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE idempotency_key = $1`, sideEffectTable), key,
	).Scan(&postCount); err != nil {
		t.Fatalf("count side effects post-replay: %v", err)
	}
	if postCount != 1 {
		t.Fatalf("side-effect rows after replay = %d, want still exactly 1 — duplicate_side_effect_prevented should have fired instead", postCount)
	}
}
