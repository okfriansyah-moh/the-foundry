// supervisor.go: docs/PLAN.md Task 32 / FND-13's liveness supervisor
// (Constitution C22, docs/foundry/docs/operations/disaster-recovery.md
// §20.10 "Liveness Supervisor").
//
// Classify is this file's core, pure decision: given one workflow's
// projected snapshot, which of the five liveness invariants (§20.10.1) it
// satisfies, or which stall condition it violates. Supervisor wraps
// Classify in a scan loop that repairs what it safely can and escalates
// the rest — but Supervisor's own dependencies (ProjectionSource,
// WorkflowController, Notifier) are interfaces this package defines and
// consumes, not implements: a live Postgres/Temporal-backed
// ProjectionSource/WorkflowController is deliberately out of this task's
// Outputs (`internal/recovery/{supervisor.go,retrypolicy.go,blocked.go}`;
// no migration and no cmd/foundryd wiring is named) and is left to
// whichever future task wires a running supervisor daemon into foundryd —
// writing that wiring blind, with no live Temporal/Postgres available to
// verify it against in this session, would be shipping unverified
// authority code, which this task's own R3 tier exists to prevent.
// Notifier is the one exception: it is satisfied directly by
// *internal/notify.Engine (Task 30), which already exists and already
// exports a matching Ingest method — no adapter needed.
package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// Condition is the liveness classification Classify assigns one
// nonterminal workflow snapshot. Healthy means the workflow satisfies one
// of disaster-recovery.md §20.10.1's five liveness invariants; every
// other value names one of docs/PLAN.md Task 32's five chaos-test stall
// modes.
type Condition int

const (
	// Healthy: RUNNING with a live heartbeat, or WAITING with a future
	// wake_at or a recognized event/human-gate subscription.
	Healthy Condition = iota
	// DeadWorker: RUNNING, but no heartbeat has been observed within
	// Config.StaleAfter — "dead worker" (the process holding this task
	// presumably crashed).
	DeadWorker
	// StuckActivity: RUNNING, heartbeat is fresh (the process is alive),
	// but no checkpoint progress has landed within Config.NoProgressAfter
	// — "stuck activity" (alive, but not advancing; Temporal's own
	// heartbeat timeout cannot detect this, because heartbeats keep
	// arriving).
	StuckActivity
	// MissingWake: WAITING, with no future wake_at and no reason in the
	// event-subscribed set — "missing wake" (nothing will ever resume
	// this workflow).
	MissingWake
	// PoisonedTask: RUNNING, and the two most recently recorded failures
	// share an identical signature — "poisoned task" (retrypolicy.go's
	// own no-progress detector would also stop here; the supervisor
	// surfaces it independently so a workflow whose retries are tracked
	// outside internal/kernel is still caught).
	PoisonedTask
	// InfiniteRetry: RUNNING, and Attempt exceeds Config.RetryBudget
	// while the workflow is still going — "infinite retry attempt" (a
	// retry loop that should have stopped per retrypolicy.go's budget,
	// but did not).
	InfiniteRetry
)

// String renders c for logs and notification text.
func (c Condition) String() string {
	switch c {
	case Healthy:
		return "healthy"
	case DeadWorker:
		return "dead-worker"
	case StuckActivity:
		return "stuck-activity"
	case MissingWake:
		return "missing-wake"
	case PoisonedTask:
		return "poisoned-task"
	case InfiniteRetry:
		return "infinite-retry"
	default:
		return "unknown"
	}
}

// Orphaned reports whether c is any non-Healthy condition —
// disaster-recovery.md §20.10.1's "anything else is an orphan."
func (c Condition) Orphaned() bool { return c != Healthy }

// eventSubscribedReasons are state.Reason values that resume a WAITING
// workflow via a registered signal or human gate rather than a scheduled
// wake_at (docs/foundry/docs/operations/disaster-recovery.md §20.10.1:
// "WAITING for a registered external event" / "WAITING for a declared
// human gate"). Every other registered wait-reason
// (internal/state/registries.go's reasonRegistry) is time-based and must
// carry a future wake_at or it is MissingWake.
var eventSubscribedReasons = map[state.Reason]bool{
	state.ReasonBudget:              true, // internal/kernel.SignalBudgetRaised
	state.ReasonHumanApproval:       true,
	state.ReasonHumanCommand:        true,
	state.ReasonExternalDeployment:  true,
	state.ReasonSecurityHold:        true,
	state.ReasonBlockedDependency:   true,
	state.ReasonUnforeseenHumanGate: true,
}

// WorkflowSnapshot is the projected view Classify needs for one
// nonterminal workflow — exactly the fields disaster-recovery.md
// §20.10.1's liveness invariants are checked against.
type WorkflowSnapshot struct {
	WorkflowID string
	Status     state.Status
	Reason     state.Reason
	// WakeAt is nil when the workflow is not scheduled to wake at a
	// specific time (state.Transition.WakeAt / workflow_status_projection
	// .wake_at).
	WakeAt *time.Time
	// Attempt is the workflow-level logical attempt counter for its
	// current task (distinct from Temporal's own per-call
	// activity.Info.Attempt; see internal/kernel/idempotency.go's
	// Attempt).
	Attempt int
	// LastHeartbeat is the most recent liveness signal observed for a
	// RUNNING workflow's active activity (e.g. Temporal's
	// activity.RecordHeartbeat, surfaced via DescribeWorkflowExecution's
	// PendingActivities). Zero means none has ever been observed.
	LastHeartbeat time.Time
	// LastProgressAt is when this workflow's checkpoint last advanced (a
	// new state.Transition landed) — disaster-recovery.md §20.10.2's
	// "task state advanced" progress signal. Zero means never.
	LastProgressAt time.Time
	// RecentFailures are this workflow's current task's failure
	// signatures, oldest first, as recorded by whatever retrypolicy.go
	// caller tracks them.
	RecentFailures []FailureSignature
}

// Config sizes Classify's stall thresholds. The zero value is usable —
// see the accessor methods for defaults.
type Config struct {
	// StaleAfter bounds how long a RUNNING workflow may go without a
	// heartbeat before it is DeadWorker. Defaults to 5m
	// (docs/foundry/docs/workflows/recovery.md §20.9.1's stale_after).
	StaleAfter time.Duration
	// NoProgressAfter bounds how long a RUNNING workflow may go without
	// checkpoint progress before it is StuckActivity. Defaults to 30m
	// (recovery.md §20.9.1's no_progress_after).
	NoProgressAfter time.Duration
	// RetryBudget bounds Attempt before a still-RUNNING workflow is
	// InfiniteRetry. Defaults to retrypolicy.go's
	// defaultBudgets[verify.ClassificationRetryable] (3).
	RetryBudget int
}

func (c Config) staleAfter() time.Duration {
	if c.StaleAfter > 0 {
		return c.StaleAfter
	}
	return 5 * time.Minute
}

func (c Config) noProgressAfter() time.Duration {
	if c.NoProgressAfter > 0 {
		return c.NoProgressAfter
	}
	return 30 * time.Minute
}

func (c Config) retryBudget() int {
	if c.RetryBudget > 0 {
		return c.RetryBudget
	}
	return defaultBudgets[verify.ClassificationRetryable]
}

// Classify assigns snap a Condition as of now, per disaster-recovery.md
// §20.10.1's five liveness invariants. Checks are ordered so each
// Condition is mutually exclusive with the others: a RUNNING workflow is
// first checked for a missing heartbeat (DeadWorker) — only once a fresh
// heartbeat proves the process itself is alive do the remaining RUNNING
// checks (PoisonedTask, InfiniteRetry, StuckActivity) apply.
func Classify(now time.Time, snap WorkflowSnapshot, cfg Config) Condition {
	switch snap.Status {
	case state.StatusRunning:
		if snap.LastHeartbeat.IsZero() || now.Sub(snap.LastHeartbeat) > cfg.staleAfter() {
			return DeadWorker
		}
		if n := len(snap.RecentFailures); n >= 2 && snap.RecentFailures[n-1].Key() == snap.RecentFailures[n-2].Key() {
			return PoisonedTask
		}
		if snap.Attempt > cfg.retryBudget() {
			return InfiniteRetry
		}
		if !snap.LastProgressAt.IsZero() && now.Sub(snap.LastProgressAt) > cfg.noProgressAfter() {
			return StuckActivity
		}
		return Healthy
	case state.StatusWaiting:
		if snap.WakeAt != nil && snap.WakeAt.After(now) {
			return Healthy
		}
		if eventSubscribedReasons[snap.Reason] {
			return Healthy
		}
		return MissingWake
	default:
		return Healthy
	}
}

// ProjectionSource lists every currently nonterminal workflow's liveness
// snapshot. Implemented by whatever wires a real Postgres
// workflow_status_projection reader (Task 14) plus a Temporal heartbeat
// source together — see this file's doc comment on why that adapter is
// not itself part of this task.
type ProjectionSource interface {
	ListNonterminal(ctx context.Context) ([]WorkflowSnapshot, error)
}

// WorkflowController repairs an orphaned RUNNING workflow via Temporal's
// own signal/reset APIs (disaster-recovery.md §20.10.3: "fence old worker
// -> load last checkpoint ... -> assign wake_at or live lease").
type WorkflowController interface {
	Reset(ctx context.Context, workflowID string) error
}

// Notifier escalates a stall the Supervisor cannot safely repair itself.
// *internal/notify.Engine (Task 30) satisfies this directly.
type Notifier interface {
	Ingest(ctx context.Context, ev notify.Event) error
}

// Action is what Supervisor did about one orphaned snapshot.
type Action string

// Action values ScanResult.Action takes.
const (
	ActionRepaired         Action = "repaired"
	ActionEscalated        Action = "escalated"
	ActionRepairFailed     Action = "repair-failed-escalated"
	ActionEscalationFailed Action = "escalation-failed"
)

// ScanResult reports one orphaned workflow's outcome from one ScanOnce
// call.
type ScanResult struct {
	WorkflowID string
	Condition  Condition
	Action     Action
	Err        error
}

// repairableConditions are the Conditions Supervisor attempts to fix via
// WorkflowController.Reset before ever escalating: process-level failures
// (a dead or hung worker) where restarting from the last checkpoint is
// plausibly sufficient. MissingWake, PoisonedTask, and InfiniteRetry are
// never auto-repaired — disaster-recovery.md §20.2's Recovery Manager
// "cannot ... retry indefinitely", and a workflow that already exhausted
// or bypassed its own retry budget must go to a human, not get another
// automatic attempt from this supervisor.
var repairableConditions = map[Condition]bool{
	DeadWorker:    true,
	StuckActivity: true,
}

// Supervisor is docs/PLAN.md Task 32's liveness supervisor: it scans
// Source for nonterminal workflows on an interval, classifies each, and
// repairs or escalates every orphan it finds. The zero value is not
// usable — Source, Controller, and Notifier are all required.
type Supervisor struct {
	Source     ProjectionSource
	Controller WorkflowController
	Notifier   Notifier
	Config     Config

	// OpsChatID/OpsChatType address the escalation notification (a P1 per
	// docs/PLAN.md Task 32 Steps: "escalate a P1 notification").
	OpsChatID   string
	OpsChatType notify.ChatType

	// Now supplies the current time. Defaults to time.Now; tests inject a
	// fixed/controllable clock.
	Now func() time.Time
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

func (s *Supervisor) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Supervisor) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Supervisor) chatType() notify.ChatType {
	if s.OpsChatType != "" {
		return s.OpsChatType
	}
	return notify.ChatPrivate
}

// ScanOnce runs one liveness scan: list every nonterminal workflow,
// classify it, and repair or escalate every orphan. It returns one
// ScanResult per orphan found (Healthy snapshots are silently skipped).
func (s *Supervisor) ScanOnce(ctx context.Context) ([]ScanResult, error) {
	now := s.now()
	snaps, err := s.Source.ListNonterminal(ctx)
	if err != nil {
		return nil, fmt.Errorf("recovery: list nonterminal workflows: %w", err)
	}

	var results []ScanResult
	for _, snap := range snaps {
		cond := Classify(now, snap, s.Config)
		if !cond.Orphaned() {
			continue
		}
		results = append(results, s.handle(ctx, snap, cond))
	}
	return results, nil
}

// handle repairs cond via Controller.Reset when it is a repairable
// condition, falling back to escalation if the repair attempt itself
// errors (a repair that cannot even be attempted is not silently
// dropped); every other condition escalates directly.
func (s *Supervisor) handle(ctx context.Context, snap WorkflowSnapshot, cond Condition) ScanResult {
	if !repairableConditions[cond] {
		return s.escalate(ctx, snap, cond, ActionEscalated)
	}
	if err := s.Controller.Reset(ctx, snap.WorkflowID); err != nil {
		s.logger().Error("recovery: repair failed, escalating", "workflow_id", snap.WorkflowID, "condition", cond.String(), "error", err)
		return s.escalate(ctx, snap, cond, ActionRepairFailed)
	}
	s.logger().Warn("recovery: repaired orphaned workflow", "workflow_id", snap.WorkflowID, "condition", cond.String())
	return ScanResult{WorkflowID: snap.WorkflowID, Condition: cond, Action: ActionRepaired}
}

func (s *Supervisor) escalate(ctx context.Context, snap WorkflowSnapshot, cond Condition, onSuccess Action) ScanResult {
	ev := notify.Event{
		Class:      notify.P1Command,
		ChatID:     s.OpsChatID,
		ChatType:   s.chatType(),
		Workflow:   snap.WorkflowID,
		Text:       fmt.Sprintf("liveness supervisor: workflow %s is %s -- needs operator attention", snap.WorkflowID, cond),
		DedupeKey:  fmt.Sprintf("liveness:%s:%s", snap.WorkflowID, cond),
		OccurredAt: s.now(),
	}
	if err := s.Notifier.Ingest(ctx, ev); err != nil {
		s.logger().Error("recovery: escalation notify failed", "workflow_id", snap.WorkflowID, "condition", cond.String(), "error", err)
		return ScanResult{WorkflowID: snap.WorkflowID, Condition: cond, Action: ActionEscalationFailed, Err: err}
	}
	return ScanResult{WorkflowID: snap.WorkflowID, Condition: cond, Action: onSuccess}
}

// Run scans every interval until ctx is cancelled, mirroring
// internal/projection.Projector.Run's shape.
func (s *Supervisor) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.ScanOnce(ctx); err != nil {
				return err
			}
		}
	}
}
