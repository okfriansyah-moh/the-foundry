package main

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Implementer invokes the implementation protocol headlessly for one card (Task 3 Step
// 4: "invoke the implementation protocol headlessly, e.g. claude -p"). The real
// implementation shells into the dev container; tests inject a fake so the runner's
// dispatch logic is verifiable without a live coding agent.
type Implementer interface {
	Implement(ctx context.Context, card *Card) error
}

// Validator runs a card's own Validation commands plus the repo-wide gate (`make test
// fitness`). Returning ok=false or a non-nil err both count as a validation failure for
// the two-consecutive-failures halt rule (Task 3 Step 4).
type Validator interface {
	Validate(ctx context.Context, card *Card) (ok bool, output string, err error)
}

// SCM performs the commit for the AUTO/approved-GATED path. This is plain `git` against
// the runner's own working tree — never internal/scm/write, which is the kernel's
// exclusive authority (Constitution C4; see doc.go).
type SCM interface {
	Commit(ctx context.Context, card *Card) error
}

// Notifier is the Telegram gate (Task 3 Steps 5-7): GATED approval, halt alerts, the
// batched AUTO digest, and the /freeze kill switch.
type Notifier interface {
	NotifyGated(ctx context.Context, card *Card, reason, validationOutput string) error
	NotifyHalt(ctx context.Context, card *Card, reason string) error
	QueueDigest(card *Card)
	FlushDigest(ctx context.Context) error
	WaitApproval(ctx context.Context, card *Card) (approved bool, err error)
	Frozen(ctx context.Context) bool
}

// Outcome is the terminal state of one RunTask call.
type Outcome struct {
	Task   int
	Tier   Tier
	Status string // auto_completed | gated_approved | gated_rejected | halted | frozen | error
	Reason string
}

const (
	defaultMaxConcurrent = 2 // Task 3 Step 2: [P] tasks with disjoint Outputs, capped at 2.
	defaultDigestEvery   = 5 // Task 3 Step 6/7: digest batch size doubles as the drift cap.
	maxAttempts          = 2 // Task 3 Step 4: two consecutive failures halts the runner.
)

// Runner drives the plan per Task 3 Steps 2-7. It never decides anything beyond what
// each card's own Risk/Rev already grants (Constitution C4/C5/C6) — see doc.go.
type Runner struct {
	Plan          *Plan
	Implementer   Implementer
	Validator     Validator
	SCM           SCM
	Notifier      Notifier
	MaxConcurrent int
	Clock         func() time.Time
}

// NewRunner wires a Runner with the plan-card defaults (concurrency cap 2).
func NewRunner(plan *Plan, impl Implementer, val Validator, scm SCM, notif Notifier) *Runner {
	return &Runner{
		Plan:          plan,
		Implementer:   impl,
		Validator:     val,
		SCM:           scm,
		Notifier:      notif,
		MaxConcurrent: defaultMaxConcurrent,
		Clock:         func() time.Time { return time.Now().UTC() },
	}
}

// SelectBatch returns up to MaxConcurrent eligible task numbers safe to dispatch
// together: [P]-marked rows with pairwise-disjoint declared Outputs, or a single
// non-[P] row if that is the next eligible candidate (Task 3 Step 2).
func (r *Runner) SelectBatch() []int {
	eligible := r.Plan.Eligible()
	if len(eligible) == 0 {
		return nil
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].Task < eligible[j].Task })

	first := eligible[0]
	if !first.Parallel {
		return []int{first.Task}
	}

	batch := []int{first.Task}
	seenOutputs := map[string]bool{}
	if card := r.Plan.Cards[first.Task]; card != nil {
		seenOutputs[card.Outputs] = true
	}
	for _, row := range eligible[1:] {
		if len(batch) >= r.MaxConcurrent {
			break
		}
		if !row.Parallel {
			continue
		}
		card := r.Plan.Cards[row.Task]
		if card == nil || seenOutputs[card.Outputs] {
			continue
		}
		batch = append(batch, row.Task)
		seenOutputs[card.Outputs] = true
	}
	return batch
}

// RunTask drives one task end to end (Task 3 Steps 3-5), retrying the
// implement-then-validate cycle once before halting on a second consecutive failure.
func (r *Runner) RunTask(ctx context.Context, taskNum int) Outcome {
	card := r.Plan.Cards[taskNum]
	if card == nil {
		return Outcome{Task: taskNum, Status: "error", Reason: "card not found"}
	}
	tier, reason := Classify(card.Risk, card.Rev)

	if r.Notifier != nil && r.Notifier.Frozen(ctx) {
		return Outcome{Task: taskNum, Tier: tier, Status: "frozen"}
	}

	var lastOutput string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := r.Implementer.Implement(ctx, card); err != nil {
			if attempt == maxAttempts {
				return r.halt(ctx, card, tier, fmt.Sprintf("implementation failed twice: %v", err))
			}
			continue
		}

		ok, output, err := r.Validator.Validate(ctx, card)
		lastOutput = output
		if err != nil || !ok {
			if attempt == maxAttempts {
				return r.halt(ctx, card, tier, "two consecutive validation failures")
			}
			continue
		}

		return r.complete(ctx, card, tier, reason, lastOutput)
	}
	return r.halt(ctx, card, tier, "exhausted retries")
}

func (r *Runner) halt(ctx context.Context, card *Card, tier Tier, reason string) Outcome {
	if r.Notifier != nil {
		_ = r.Notifier.NotifyHalt(ctx, card, reason)
	}
	return Outcome{Task: card.Task, Tier: tier, Status: "halted", Reason: reason}
}

func (r *Runner) complete(ctx context.Context, card *Card, tier Tier, gatedReason, validationOutput string) Outcome {
	switch tier {
	case Auto:
		if err := r.SCM.Commit(ctx, card); err != nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: err.Error()}
		}
		if err := r.Plan.MarkDone(card.Task, r.Clock().Format("2006-01-02")); err != nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: err.Error()}
		}
		if r.Notifier != nil {
			r.Notifier.QueueDigest(card)
		}
		return Outcome{Task: card.Task, Tier: tier, Status: "auto_completed"}

	case Gated:
		if r.Notifier == nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: "gated task requires a notifier"}
		}
		if err := r.Notifier.NotifyGated(ctx, card, gatedReason, validationOutput); err != nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: err.Error()}
		}
		approved, err := r.Notifier.WaitApproval(ctx, card)
		if err != nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: err.Error()}
		}
		if !approved {
			return Outcome{Task: card.Task, Tier: tier, Status: "gated_rejected"}
		}
		if err := r.SCM.Commit(ctx, card); err != nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: err.Error()}
		}
		if err := r.Plan.MarkDone(card.Task, r.Clock().Format("2006-01-02")); err != nil {
			return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: err.Error()}
		}
		return Outcome{Task: card.Task, Tier: tier, Status: "gated_approved"}

	default:
		return Outcome{Task: card.Task, Tier: tier, Status: "error", Reason: "unknown tier"}
	}
}

// RunBatch dispatches a batch of task numbers concurrently, bounded by however SelectBatch
// built the batch, and returns their outcomes in the same order.
func (r *Runner) RunBatch(ctx context.Context, batch []int) []Outcome {
	outcomes := make([]Outcome, len(batch))
	done := make(chan int, len(batch))
	for i, task := range batch {
		i, task := i, task
		go func() {
			outcomes[i] = r.RunTask(ctx, task)
			done <- i
		}()
	}
	for range batch {
		<-done
	}
	return outcomes
}

// RunAll drives the eligible backlog one batch at a time until nothing is left, a task
// halts, is frozen, or is rejected — mirroring the "never keeps trying silently" rule
// (Task 3 Step 4 / Constitution C22 no-progress rule): none of those three outcomes are
// retried automatically.
func (r *Runner) RunAll(ctx context.Context) []Outcome {
	var all []Outcome
	for {
		batch := r.SelectBatch()
		if len(batch) == 0 {
			break
		}
		outcomes := r.RunBatch(ctx, batch)
		all = append(all, outcomes...)

		stop := false
		for _, o := range outcomes {
			if o.Status == "halted" || o.Status == "frozen" || o.Status == "gated_rejected" {
				stop = true
			}
		}
		if r.Notifier != nil {
			_ = r.Notifier.FlushDigest(ctx)
		}
		if stop {
			break
		}
	}
	return all
}
