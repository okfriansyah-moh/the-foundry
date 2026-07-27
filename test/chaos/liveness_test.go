// Package chaos holds docs/PLAN.md Task 32 / FND-13's chaos test: it
// seeds internal/recovery.Supervisor with each of the five named stall
// modes (dead worker, stuck activity, missing wake, poisoned task,
// infinite retry attempt) via fakes — never a real Temporal/Postgres, so
// detection is deterministic and fast rather than flaky — and asserts
// each is both correctly classified and correctly repaired/escalated
// within 2x the scan interval (this task's Acceptance bar).
package chaos

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// scanInterval is the Supervisor.Run interval every stall-mode test uses.
// detectionBudget (2x this, per the task's Acceptance bar) is the hard
// timeout waitFor enforces.
const scanInterval = 100 * time.Millisecond

var detectionBudget = 2 * scanInterval

// fakeSource is a recovery.ProjectionSource returning a fixed snapshot
// set — the chaos "world state" one stall mode seeds.
type fakeSource struct {
	mu    sync.Mutex
	snaps []recovery.WorkflowSnapshot
}

func (f *fakeSource) ListNonterminal(context.Context) ([]recovery.WorkflowSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recovery.WorkflowSnapshot, len(f.snaps))
	copy(out, f.snaps)
	return out, nil
}

// recordingController is a recovery.WorkflowController that records every
// Reset call, simulating "signal/reset per Temporal APIs" without a real
// Temporal server.
type recordingController struct {
	mu     sync.Mutex
	resets []string
}

func (c *recordingController) Reset(_ context.Context, workflowID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resets = append(c.resets, workflowID)
	return nil
}

func (c *recordingController) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.resets...)
}

// recordingNotifier is a recovery.Notifier that records every escalated
// notify.Event, standing in for internal/notify.Engine's real Ingest.
type recordingNotifier struct {
	mu     sync.Mutex
	events []notify.Event
}

func (n *recordingNotifier) Ingest(_ context.Context, ev notify.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, ev)
	return nil
}

func (n *recordingNotifier) calls() []notify.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]notify.Event(nil), n.events...)
}

// waitFor polls check every scanInterval/10 until it returns true, and
// fails the test if it does not within budget — the mechanism that turns
// this task's Acceptance bar ("detected in under 2x the scan interval")
// into an enforced assertion rather than a hopeful comment.
func waitFor(t *testing.T, budget time.Duration, check func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	poll := time.NewTicker(scanInterval / 10)
	defer poll.Stop()
	deadline := time.After(budget)
	for {
		if check() {
			return time.Since(start)
		}
		select {
		case <-poll.C:
		case <-deadline:
			t.Fatalf("condition not observed within %s", budget)
			return 0
		}
	}
}

// runOne wires a Supervisor around one seeded snapshot, runs it in the
// background, and returns the fakes plus a cancel func the caller must
// defer.
func runOne(t *testing.T, now time.Time, snap recovery.WorkflowSnapshot, cfg recovery.Config) (*recordingController, *recordingNotifier, context.CancelFunc) {
	t.Helper()
	source := &fakeSource{snaps: []recovery.WorkflowSnapshot{snap}}
	controller := &recordingController{}
	notifier := &recordingNotifier{}
	sup := &recovery.Supervisor{
		Source:     source,
		Controller: controller,
		Notifier:   notifier,
		Config:     cfg,
		OpsChatID:  "ops-chat",
		Now:        func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = sup.Run(ctx, scanInterval) }()
	return controller, notifier, cancel
}

func defaultConfig() recovery.Config {
	return recovery.Config{StaleAfter: 5 * time.Minute, NoProgressAfter: 30 * time.Minute, RetryBudget: 3}
}

// TestLiveness_DeadWorker seeds a RUNNING workflow whose heartbeat is far
// older than StaleAfter — a worker that crashed mid-task — and expects
// Classify to call it DeadWorker and the Supervisor to repair it (Reset)
// rather than escalate.
func TestLiveness_DeadWorker(t *testing.T) {
	now := time.Now()
	cfg := defaultConfig()
	snap := recovery.WorkflowSnapshot{
		WorkflowID:     "wf-dead-worker",
		Status:         state.StatusRunning,
		LastHeartbeat:  now.Add(-10 * time.Minute),
		LastProgressAt: now.Add(-1 * time.Second),
	}

	if got := recovery.Classify(now, snap, cfg); got != recovery.DeadWorker {
		t.Fatalf("Classify() = %v, want DeadWorker", got)
	}

	controller, notifier, cancel := runOne(t, now, snap, cfg)
	defer cancel()

	elapsed := waitFor(t, detectionBudget, func() bool { return len(controller.calls()) > 0 })
	t.Logf("dead-worker repaired in %s (budget %s)", elapsed, detectionBudget)

	if calls := controller.calls(); len(calls) == 0 || calls[0] != snap.WorkflowID {
		t.Fatalf("Controller.Reset calls = %v, want [%s]", calls, snap.WorkflowID)
	}
	if events := notifier.calls(); len(events) != 0 {
		t.Fatalf("unexpected escalation for a repairable condition: %v", events)
	}
}

// TestLiveness_StuckActivity seeds a RUNNING workflow with a fresh
// heartbeat (the worker is alive) but no checkpoint progress in a very
// long time — the case Temporal's own heartbeat timeout cannot catch,
// because heartbeats keep arriving even though the task is stuck.
func TestLiveness_StuckActivity(t *testing.T) {
	now := time.Now()
	cfg := defaultConfig()
	snap := recovery.WorkflowSnapshot{
		WorkflowID:     "wf-stuck-activity",
		Status:         state.StatusRunning,
		LastHeartbeat:  now.Add(-1 * time.Second),
		LastProgressAt: now.Add(-1 * time.Hour),
	}

	if got := recovery.Classify(now, snap, cfg); got != recovery.StuckActivity {
		t.Fatalf("Classify() = %v, want StuckActivity", got)
	}

	controller, notifier, cancel := runOne(t, now, snap, cfg)
	defer cancel()

	elapsed := waitFor(t, detectionBudget, func() bool { return len(controller.calls()) > 0 })
	t.Logf("stuck-activity repaired in %s (budget %s)", elapsed, detectionBudget)

	if calls := controller.calls(); len(calls) == 0 || calls[0] != snap.WorkflowID {
		t.Fatalf("Controller.Reset calls = %v, want [%s]", calls, snap.WorkflowID)
	}
	if events := notifier.calls(); len(events) != 0 {
		t.Fatalf("unexpected escalation for a repairable condition: %v", events)
	}
}

// TestLiveness_MissingWake seeds a WAITING workflow with a time-based
// wait reason (rate-reset) but no wake_at at all — nothing will ever
// resume it — and expects escalation, since guessing a wake time or
// signal for an anomaly like this is not a safe automatic repair.
func TestLiveness_MissingWake(t *testing.T) {
	now := time.Now()
	cfg := defaultConfig()
	snap := recovery.WorkflowSnapshot{
		WorkflowID: "wf-missing-wake",
		Status:     state.StatusWaiting,
		Reason:     state.ReasonRateReset,
		WakeAt:     nil,
	}

	if got := recovery.Classify(now, snap, cfg); got != recovery.MissingWake {
		t.Fatalf("Classify() = %v, want MissingWake", got)
	}

	controller, notifier, cancel := runOne(t, now, snap, cfg)
	defer cancel()

	elapsed := waitFor(t, detectionBudget, func() bool { return len(notifier.calls()) > 0 })
	t.Logf("missing-wake escalated in %s (budget %s)", elapsed, detectionBudget)

	events := notifier.calls()
	if len(events) == 0 || events[0].Workflow != snap.WorkflowID {
		t.Fatalf("Notifier.Ingest calls = %v, want workflow %s", events, snap.WorkflowID)
	}
	if events[0].Class != notify.P1Command {
		t.Fatalf("escalation Class = %v, want P1Command", events[0].Class)
	}
	if calls := controller.calls(); len(calls) != 0 {
		t.Fatalf("unexpected repair attempt for a non-repairable condition: %v", calls)
	}
}

// TestLiveness_PoisonedTask seeds a RUNNING workflow whose two most
// recent failure signatures are identical — retrypolicy.go's no-progress
// detector's own trigger condition — and expects escalation, not another
// automatic repair attempt.
func TestLiveness_PoisonedTask(t *testing.T) {
	now := time.Now()
	cfg := defaultConfig()
	snap := recovery.WorkflowSnapshot{
		WorkflowID:     "wf-poisoned-task",
		Status:         state.StatusRunning,
		LastHeartbeat:  now.Add(-1 * time.Second),
		LastProgressAt: now.Add(-1 * time.Second),
		RecentFailures: []recovery.FailureSignature{
			{Classification: verify.ClassificationVerificationFailed, Detail: "make test: exit 1"},
			{Classification: verify.ClassificationVerificationFailed, Detail: "make test: exit 1"},
		},
	}

	if got := recovery.Classify(now, snap, cfg); got != recovery.PoisonedTask {
		t.Fatalf("Classify() = %v, want PoisonedTask", got)
	}

	controller, notifier, cancel := runOne(t, now, snap, cfg)
	defer cancel()

	elapsed := waitFor(t, detectionBudget, func() bool { return len(notifier.calls()) > 0 })
	t.Logf("poisoned-task escalated in %s (budget %s)", elapsed, detectionBudget)

	events := notifier.calls()
	if len(events) == 0 || events[0].Workflow != snap.WorkflowID {
		t.Fatalf("Notifier.Ingest calls = %v, want workflow %s", events, snap.WorkflowID)
	}
	if calls := controller.calls(); len(calls) != 0 {
		t.Fatalf("unexpected repair attempt for a non-repairable condition: %v", calls)
	}
}

// TestLiveness_InfiniteRetryAttempt seeds a RUNNING workflow whose
// Attempt has climbed past the configured RetryBudget while the workflow
// is still going — a retry loop that should have stopped already — and
// expects escalation.
func TestLiveness_InfiniteRetryAttempt(t *testing.T) {
	now := time.Now()
	cfg := defaultConfig()
	snap := recovery.WorkflowSnapshot{
		WorkflowID:     "wf-infinite-retry",
		Status:         state.StatusRunning,
		LastHeartbeat:  now.Add(-1 * time.Second),
		LastProgressAt: now.Add(-1 * time.Second),
		Attempt:        7,
	}

	if got := recovery.Classify(now, snap, cfg); got != recovery.InfiniteRetry {
		t.Fatalf("Classify() = %v, want InfiniteRetry", got)
	}

	controller, notifier, cancel := runOne(t, now, snap, cfg)
	defer cancel()

	elapsed := waitFor(t, detectionBudget, func() bool { return len(notifier.calls()) > 0 })
	t.Logf("infinite-retry escalated in %s (budget %s)", elapsed, detectionBudget)

	events := notifier.calls()
	if len(events) == 0 || events[0].Workflow != snap.WorkflowID {
		t.Fatalf("Notifier.Ingest calls = %v, want workflow %s", events, snap.WorkflowID)
	}
	if calls := controller.calls(); len(calls) != 0 {
		t.Fatalf("unexpected repair attempt for a non-repairable condition: %v", calls)
	}
}

// TestLiveness_HealthyWorkflowsAreNotOrphaned is a negative control: a
// RUNNING workflow with a fresh heartbeat and recent progress, and a
// WAITING workflow with a future wake_at, must never be classified as an
// orphan or trigger either repair or escalation.
func TestLiveness_HealthyWorkflowsAreNotOrphaned(t *testing.T) {
	now := time.Now()
	cfg := defaultConfig()
	future := now.Add(1 * time.Hour)
	snaps := []recovery.WorkflowSnapshot{
		{
			WorkflowID:     "wf-healthy-running",
			Status:         state.StatusRunning,
			LastHeartbeat:  now.Add(-1 * time.Second),
			LastProgressAt: now.Add(-1 * time.Second),
		},
		{
			WorkflowID: "wf-healthy-waiting",
			Status:     state.StatusWaiting,
			Reason:     state.ReasonRateReset,
			WakeAt:     &future,
		},
	}

	for _, snap := range snaps {
		if got := recovery.Classify(now, snap, cfg); got != recovery.Healthy {
			t.Fatalf("Classify(%s) = %v, want Healthy", snap.WorkflowID, got)
		}
	}

	source := &fakeSource{snaps: snaps}
	controller := &recordingController{}
	notifier := &recordingNotifier{}
	sup := &recovery.Supervisor{
		Source:     source,
		Controller: controller,
		Notifier:   notifier,
		Config:     cfg,
		Now:        func() time.Time { return now },
	}

	results, err := sup.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("ScanOnce results = %v, want none for healthy snapshots", results)
	}
	if calls := controller.calls(); len(calls) != 0 {
		t.Fatalf("unexpected repair for healthy snapshots: %v", calls)
	}
	if events := notifier.calls(); len(events) != 0 {
		t.Fatalf("unexpected escalation for healthy snapshots: %v", events)
	}
}
