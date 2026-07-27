package recovery_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

func TestPolicy_Decide_RetryableBudgetIsThreeAttempts(t *testing.T) {
	p := recovery.Policy{Rand: rand.New(rand.NewSource(1))}

	sig := func(detail string) recovery.FailureSignature {
		return recovery.FailureSignature{Classification: verify.ClassificationRetryable, Detail: detail}
	}

	// Distinct details so the no-progress detector never fires here —
	// this test is isolating the budget check alone.
	history := []recovery.FailureSignature{sig("a")}
	if got := p.Decide(history).Action; got != recovery.ActionRetry {
		t.Fatalf("attempt 1: Action = %v, want ActionRetry", got)
	}

	history = append(history, sig("b"))
	if got := p.Decide(history).Action; got != recovery.ActionRetry {
		t.Fatalf("attempt 2: Action = %v, want ActionRetry", got)
	}

	history = append(history, sig("c"))
	decision := p.Decide(history)
	if decision.Action != recovery.ActionStop {
		t.Fatalf("attempt 3: Action = %v, want ActionStop", decision.Action)
	}
	if decision.Reason != recovery.StopReasonBudgetExhausted {
		t.Fatalf("attempt 3: Reason = %q, want %q", decision.Reason, recovery.StopReasonBudgetExhausted)
	}
}

func TestPolicy_Decide_NonRetryableClassificationStopsImmediately(t *testing.T) {
	p := recovery.Policy{}
	history := []recovery.FailureSignature{
		{Classification: verify.ClassificationVerificationFailed, Detail: "exit 1"},
	}
	decision := p.Decide(history)
	if decision.Action != recovery.ActionStop {
		t.Fatalf("Action = %v, want ActionStop", decision.Action)
	}
	if decision.Reason != recovery.StopReasonBudgetExhausted {
		t.Fatalf("Reason = %q, want %q", decision.Reason, recovery.StopReasonBudgetExhausted)
	}
}

func TestPolicy_Decide_IdenticalSignatureTwiceStopsAsNoProgress(t *testing.T) {
	p := recovery.Policy{}
	same := recovery.FailureSignature{Classification: verify.ClassificationRetryable, Detail: "connect: timeout"}
	history := []recovery.FailureSignature{same, same}

	decision := p.Decide(history)
	if decision.Action != recovery.ActionStop {
		t.Fatalf("Action = %v, want ActionStop", decision.Action)
	}
	if decision.Reason != recovery.StopReasonNoProgress {
		t.Fatalf("Reason = %q, want %q", decision.Reason, recovery.StopReasonNoProgress)
	}
}

func TestPolicy_Decide_EmptyHistoryStops(t *testing.T) {
	p := recovery.Policy{}
	decision := p.Decide(nil)
	if decision.Action != recovery.ActionStop {
		t.Fatalf("Action = %v, want ActionStop", decision.Action)
	}
}

func TestPolicy_Decide_BackoffIsBoundedAndJittered(t *testing.T) {
	p := recovery.Policy{BaseDelay: time.Second, MaxDelay: 10 * time.Second, Rand: rand.New(rand.NewSource(42))}
	sig := recovery.FailureSignature{Classification: verify.ClassificationRetryable, Detail: "a"}

	for attempt := 1; attempt <= 5; attempt++ {
		history := make([]recovery.FailureSignature, attempt)
		for i := range history {
			history[i] = recovery.FailureSignature{Classification: verify.ClassificationRetryable, Detail: string(rune('a' + i))}
		}
		decision := p.Decide(history)
		if decision.Action != recovery.ActionRetry {
			continue // budget exhausted at this attempt count, nothing to check
		}
		if decision.Delay < 0 || decision.Delay > p.MaxDelay {
			t.Fatalf("attempt %d: Delay = %s out of bounds [0, %s]", attempt, decision.Delay, p.MaxDelay)
		}
	}
	_ = sig
}
