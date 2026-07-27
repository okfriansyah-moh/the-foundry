// retrypolicy.go: docs/PLAN.md Task 32 / FND-13's retry-policy engine
// (Constitution C22, docs/foundry/docs/workflows/recovery.md §20.9 "Retry
// policy without stalling or hot-looping").
//
// This engine answers exactly one question for one task's failure
// history: retry (and how long to wait), or stop. It never itself
// retries, signals Temporal, or writes state — internal/kernel's workflow
// code is the only place a decision like this becomes a side effect
// (Constitution C4); this package is deliberately pure so its policy can
// be unit-tested without a workflow, an activity, or a database.
package recovery

import (
	"math/rand"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// FailureSignature is one failed attempt's fingerprint: the deterministic
// failure classification internal/verify.Evaluate already assigns
// (docs/PLAN.md Task 13 Step 4), plus enough detail to tell two failures
// of the same classification apart (or recognize they are identical).
type FailureSignature struct {
	Classification verify.Classification
	// Detail disambiguates within one Classification — e.g. which
	// command failed, or a short error digest. Two signatures with the
	// same Classification and Detail are "identical" for the no-progress
	// detector below.
	Detail string
	// MissingDependency and ContradictorySpec are impossibility flags a
	// caller attaches only when it has independently established —
	// through its own evidence, not by this package inferring it from
	// Detail's text — that retrying this task can never succeed. See
	// blocked.go's doc comment for why this package never string-sniffs
	// Detail to invent these itself.
	MissingDependency bool
	ContradictorySpec bool
	// EvidenceRefs are evidence.Store bundle IDs (or other durable refs)
	// substantiating the impossibility flags above. blocked.go's
	// ProvenBlocked.Validate requires this to be non-empty whenever
	// either flag is set — Constitution C22 forbids a PROVEN_BLOCKED with
	// no evidence.
	EvidenceRefs []string
}

// Key returns a stable fingerprint for equality comparison: two
// signatures with the same Key are the "identical failure" the
// no-progress detector (Decide) checks for.
func (s FailureSignature) Key() string {
	return string(s.Classification) + "|" + s.Detail
}

// RetryAction is Decide's verdict.
type RetryAction int

const (
	// ActionRetry means try again after Delay.
	ActionRetry RetryAction = iota
	// ActionStop means do not retry — the caller must terminate the task
	// (and, via blocked.go's Evaluate, decide whether that termination is
	// a plain FAILED or a PROVEN_BLOCKED).
	ActionStop
)

// String renders a for logs.
func (a RetryAction) String() string {
	if a == ActionRetry {
		return "retry"
	}
	return "stop"
}

// Decide's StopReason values.
const (
	StopReasonNoProgress      = "no-progress"
	StopReasonBudgetExhausted = "budget-exhausted"
)

// RetryDecision is Decide's result.
type RetryDecision struct {
	Action RetryAction
	// Delay is the backoff wait before the next attempt. Meaningful only
	// when Action == ActionRetry.
	Delay time.Duration
	// Reason explains an ActionStop (StopReasonNoProgress or
	// StopReasonBudgetExhausted). Empty for ActionRetry.
	Reason string
}

// defaultBudgets is docs/PLAN.md Task 32 Step's literal per-classification
// budget: "retryable: 3 attempts". Every other classification defaults to
// budget 0 — deterministic failures (verification-failed,
// policy-violation) and internal/verify's own no-progress classification
// are not fixed by retrying (docs/foundry/docs/workflows/recovery.md
// §20.9.2: "Deterministic compile/test failure: ... provider retry alone
// is not a solution"), so Policy stops on the first occurrence rather than
// spending a budget on them.
var defaultBudgets = map[verify.Classification]int{
	verify.ClassificationRetryable: 3,
}

// Policy is a configured retry-policy engine. The zero value is usable: it
// applies defaultBudgets and a 1s-base/2x/full-jitter backoff capped at
// 30s (chosen to match internal/kernel/workflow.go's own activity-level
// RetryPolicy shape — 1s initial interval, 2x backoff coefficient —
// rather than recovery.md §20.9.1's 30s-base workflow-lifetime numbers,
// which govern a different, higher-level policy layer than this
// task-level engine).
type Policy struct {
	// Budgets overrides defaultBudgets when non-nil.
	Budgets map[verify.Classification]int
	// BaseDelay is the first retry's backoff base. Defaults to 1s.
	BaseDelay time.Duration
	// MaxDelay caps backoff growth. Defaults to 30s.
	MaxDelay time.Duration
	// Rand supplies jitter. Defaults to a package-level source seeded
	// from the current time; tests inject a seeded *rand.Rand for
	// deterministic backoff assertions.
	Rand *rand.Rand
}

func (p Policy) budgetFor(c verify.Classification) int {
	if p.Budgets != nil {
		return p.Budgets[c]
	}
	return defaultBudgets[c]
}

func (p Policy) baseDelay() time.Duration {
	if p.BaseDelay > 0 {
		return p.BaseDelay
	}
	return time.Second
}

func (p Policy) maxDelay() time.Duration {
	if p.MaxDelay > 0 {
		return p.MaxDelay
	}
	return 30 * time.Second
}

func (p Policy) rand() *rand.Rand {
	if p.Rand != nil {
		return p.Rand
	}
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// Decide applies this Policy to history, the ordered (oldest-first)
// FailureSignatures recorded so far for one task, and returns whether to
// retry and, if so, how long to wait.
//
// Check order, both named in docs/PLAN.md Task 32's Steps:
//  1. No-progress detector: the last two signatures are identical (same
//     Key()) — "identical failure signature twice" — stop regardless of
//     remaining budget, because retrying an unchanged failure is not
//     progress.
//  2. Budget: the last signature's classification attempt budget
//     (len(history) counts the attempt just recorded) is exhausted — stop.
//
// An empty history is a caller error (Decide is only meaningful after at
// least one recorded failure) and is treated as ActionStop/no-progress
// rather than panicking or retrying blindly.
func (p Policy) Decide(history []FailureSignature) RetryDecision {
	if len(history) == 0 {
		return RetryDecision{Action: ActionStop, Reason: StopReasonNoProgress}
	}
	if n := len(history); n >= 2 && history[n-1].Key() == history[n-2].Key() {
		return RetryDecision{Action: ActionStop, Reason: StopReasonNoProgress}
	}

	last := history[len(history)-1]
	budget := p.budgetFor(last.Classification)
	if len(history) >= budget {
		return RetryDecision{Action: ActionStop, Reason: StopReasonBudgetExhausted}
	}

	return RetryDecision{Action: ActionRetry, Delay: p.backoff(len(history))}
}

// backoff computes a full-jitter exponential delay for the attempt-th
// retry (attempt is 1-indexed, matching len(history) at call time):
// sleep = random(0, min(MaxDelay, BaseDelay*2^(attempt-1))) — the "full
// jitter" formula from the AWS Architecture Blog's backoff/jitter
// article, chosen because it is the standard, well-analyzed choice for
// avoiding synchronized retry storms, not a bespoke formula invented here.
func (p Policy) backoff(attempt int) time.Duration {
	window := p.baseDelay() << uint(attempt-1) // baseDelay * 2^(attempt-1)
	if window <= 0 || window > p.maxDelay() {
		window = p.maxDelay()
	}
	if window <= 0 {
		return 0
	}
	return time.Duration(p.rand().Int63n(int64(window) + 1))
}
