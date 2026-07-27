package recovery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func TestClassify_TableDriven(t *testing.T) {
	now := time.Now()
	cfg := recovery.Config{StaleAfter: 5 * time.Minute, NoProgressAfter: 30 * time.Minute, RetryBudget: 3}
	future := now.Add(time.Hour)
	past := now.Add(-time.Minute)

	tests := []struct {
		name string
		snap recovery.WorkflowSnapshot
		want recovery.Condition
	}{
		{
			name: "running fresh heartbeat and progress is healthy",
			snap: recovery.WorkflowSnapshot{Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now},
			want: recovery.Healthy,
		},
		{
			name: "running never heartbeated is dead worker",
			snap: recovery.WorkflowSnapshot{Status: state.StatusRunning},
			want: recovery.DeadWorker,
		},
		{
			name: "running stale heartbeat is dead worker",
			snap: recovery.WorkflowSnapshot{Status: state.StatusRunning, LastHeartbeat: now.Add(-6 * time.Minute)},
			want: recovery.DeadWorker,
		},
		{
			name: "running fresh heartbeat stale progress is stuck activity",
			snap: recovery.WorkflowSnapshot{Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now.Add(-31 * time.Minute)},
			want: recovery.StuckActivity,
		},
		{
			name: "running attempt over budget is infinite retry",
			snap: recovery.WorkflowSnapshot{Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now, Attempt: 4},
			want: recovery.InfiniteRetry,
		},
		{
			name: "running attempt exactly at budget is healthy",
			snap: recovery.WorkflowSnapshot{Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now, Attempt: 3},
			want: recovery.Healthy,
		},
		{
			name: "running identical last two failures is poisoned task",
			snap: recovery.WorkflowSnapshot{
				Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now,
				RecentFailures: []recovery.FailureSignature{
					{Detail: "x"}, {Detail: "y"}, {Detail: "z"}, {Detail: "z"},
				},
			},
			want: recovery.PoisonedTask,
		},
		{
			name: "running distinct last two failures is healthy",
			snap: recovery.WorkflowSnapshot{
				Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now,
				RecentFailures: []recovery.FailureSignature{{Detail: "x"}, {Detail: "y"}},
			},
			want: recovery.Healthy,
		},
		{
			name: "waiting with future wake_at is healthy",
			snap: recovery.WorkflowSnapshot{Status: state.StatusWaiting, Reason: state.ReasonRateReset, WakeAt: &future},
			want: recovery.Healthy,
		},
		{
			name: "waiting with past wake_at and no subscription is missing wake",
			snap: recovery.WorkflowSnapshot{Status: state.StatusWaiting, Reason: state.ReasonRateReset, WakeAt: &past},
			want: recovery.MissingWake,
		},
		{
			name: "waiting with no wake_at but event-subscribed reason is healthy",
			snap: recovery.WorkflowSnapshot{Status: state.StatusWaiting, Reason: state.ReasonBudget, WakeAt: nil},
			want: recovery.Healthy,
		},
		{
			name: "waiting with no wake_at and no recognized reason is missing wake",
			snap: recovery.WorkflowSnapshot{Status: state.StatusWaiting, Reason: "", WakeAt: nil},
			want: recovery.MissingWake,
		},
		{
			name: "terminal status is healthy (not this supervisor's concern)",
			snap: recovery.WorkflowSnapshot{Status: state.StatusSucceeded},
			want: recovery.Healthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recovery.Classify(now, tt.snap, cfg); got != tt.want {
				t.Fatalf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCondition_OrphanedAndString(t *testing.T) {
	if recovery.Healthy.Orphaned() {
		t.Fatal("Healthy.Orphaned() = true, want false")
	}
	for _, c := range []recovery.Condition{recovery.DeadWorker, recovery.StuckActivity, recovery.MissingWake, recovery.PoisonedTask, recovery.InfiniteRetry} {
		if !c.Orphaned() {
			t.Fatalf("%v.Orphaned() = false, want true", c)
		}
		if c.String() == "" || c.String() == "unknown" {
			t.Fatalf("%v.String() = %q, want a named condition", c, c.String())
		}
	}
}

// stubSource is a minimal recovery.ProjectionSource for direct
// ScanOnce/handle-path tests (distinct from the chaos test's fakes, which
// live in test/chaos and exercise the full Run loop).
type stubSource struct {
	snaps []recovery.WorkflowSnapshot
	err   error
}

func (s stubSource) ListNonterminal(context.Context) ([]recovery.WorkflowSnapshot, error) {
	return s.snaps, s.err
}

type stubController struct {
	err   error
	calls []string
}

func (s *stubController) Reset(_ context.Context, workflowID string) error {
	s.calls = append(s.calls, workflowID)
	return s.err
}

type stubNotifier struct {
	err   error
	calls []notify.Event
}

func (s *stubNotifier) Ingest(_ context.Context, ev notify.Event) error {
	s.calls = append(s.calls, ev)
	return s.err
}

func TestScanOnce_PropagatesSourceError(t *testing.T) {
	sup := &recovery.Supervisor{
		Source:     stubSource{err: errors.New("boom")},
		Controller: &stubController{},
		Notifier:   &stubNotifier{},
	}
	if _, err := sup.ScanOnce(context.Background()); err == nil {
		t.Fatal("ScanOnce() error = nil, want non-nil")
	}
}

func TestScanOnce_RepairFailureFallsBackToEscalation(t *testing.T) {
	now := time.Now()
	snap := recovery.WorkflowSnapshot{WorkflowID: "wf-1", Status: state.StatusRunning} // no heartbeat -> DeadWorker
	controller := &stubController{err: errors.New("temporal unreachable")}
	notifier := &stubNotifier{}
	sup := &recovery.Supervisor{
		Source:     stubSource{snaps: []recovery.WorkflowSnapshot{snap}},
		Controller: controller,
		Notifier:   notifier,
		Now:        func() time.Time { return now },
	}

	results, err := sup.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Action != recovery.ActionRepairFailed {
		t.Fatalf("Action = %v, want ActionRepairFailed", results[0].Action)
	}
	if len(controller.calls) != 1 {
		t.Fatalf("Controller.Reset calls = %d, want 1 (repair must still be attempted)", len(controller.calls))
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("Notifier.Ingest calls = %d, want 1 (must escalate after failed repair)", len(notifier.calls))
	}
}

func TestScanOnce_EscalationFailureIsReported(t *testing.T) {
	now := time.Now()
	snap := recovery.WorkflowSnapshot{WorkflowID: "wf-2", Status: state.StatusWaiting} // no reason, no wake_at -> MissingWake
	notifier := &stubNotifier{err: errors.New("telegram down")}
	sup := &recovery.Supervisor{
		Source:     stubSource{snaps: []recovery.WorkflowSnapshot{snap}},
		Controller: &stubController{},
		Notifier:   notifier,
		Now:        func() time.Time { return now },
	}

	results, err := sup.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if len(results) != 1 || results[0].Action != recovery.ActionEscalationFailed {
		t.Fatalf("results = %+v, want one ActionEscalationFailed", results)
	}
	if results[0].Err == nil {
		t.Fatal("ScanResult.Err = nil, want the notifier's error surfaced")
	}
}

func TestScanOnce_SkipsHealthySnapshots(t *testing.T) {
	now := time.Now()
	snap := recovery.WorkflowSnapshot{WorkflowID: "wf-3", Status: state.StatusRunning, LastHeartbeat: now, LastProgressAt: now}
	controller := &stubController{}
	notifier := &stubNotifier{}
	sup := &recovery.Supervisor{
		Source:     stubSource{snaps: []recovery.WorkflowSnapshot{snap}},
		Controller: controller,
		Notifier:   notifier,
		Now:        func() time.Time { return now },
	}

	results, err := sup.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none for a healthy snapshot", results)
	}
	if len(controller.calls) != 0 || len(notifier.calls) != 0 {
		t.Fatal("healthy snapshot must never trigger repair or escalation")
	}
}

func TestSupervisor_Run_StopsOnContextCancel(t *testing.T) {
	sup := &recovery.Supervisor{
		Source:     stubSource{},
		Controller: &stubController{},
		Notifier:   &stubNotifier{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, time.Millisecond) }()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run() error = nil, want context.Canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
