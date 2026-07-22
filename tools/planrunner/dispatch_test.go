package main

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// --- test doubles ---

type stubImplementer struct {
	mu       sync.Mutex
	failFor  map[int]bool
	implCall map[int]int
}

func newStubImplementer(failFor ...int) *stubImplementer {
	m := map[int]bool{}
	for _, t := range failFor {
		m[t] = true
	}
	return &stubImplementer{failFor: m, implCall: map[int]int{}}
}

func (s *stubImplementer) Implement(ctx context.Context, card *Card) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.implCall[card.Task]++
	if s.failFor[card.Task] {
		return errImplement
	}
	return nil
}

var errImplement = errors.New("stub implement failure")

type stubValidator struct {
	mu      sync.Mutex
	failFor map[int]bool
	calls   map[int]int
}

func newStubValidator(failFor ...int) *stubValidator {
	m := map[int]bool{}
	for _, t := range failFor {
		m[t] = true
	}
	return &stubValidator{failFor: m, calls: map[int]int{}}
}

func (s *stubValidator) Validate(ctx context.Context, card *Card) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[card.Task]++
	if s.failFor[card.Task] {
		return false, "stub validation failure", nil
	}
	return true, "stub validation ok", nil
}

type stubSCM struct {
	mu      sync.Mutex
	commits []int
	failFor map[int]bool
}

func newStubSCM() *stubSCM { return &stubSCM{} }

func (s *stubSCM) Commit(ctx context.Context, card *Card) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFor != nil && s.failFor[card.Task] {
		return errImplement
	}
	s.commits = append(s.commits, card.Task)
	return nil
}

type stubNotifier struct {
	mu          sync.Mutex
	autoApprove bool
	frozen      bool
	gatedCalls  []int
	haltCalls   []int
	digest      []int
}

func (s *stubNotifier) NotifyGated(ctx context.Context, card *Card, reason, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gatedCalls = append(s.gatedCalls, card.Task)
	return nil
}

func (s *stubNotifier) NotifyHalt(ctx context.Context, card *Card, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.haltCalls = append(s.haltCalls, card.Task)
	return nil
}

func (s *stubNotifier) QueueDigest(card *Card) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.digest = append(s.digest, card.Task)
}

func (s *stubNotifier) FlushDigest(ctx context.Context) error { return nil }

func (s *stubNotifier) WaitApproval(ctx context.Context, card *Card) (bool, error) {
	return s.autoApprove, nil
}

func (s *stubNotifier) Frozen(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frozen
}

// --- tests ---

func TestRunTaskAutoCompletes(t *testing.T) {
	plan := loadFixture(t)
	impl := newStubImplementer()
	val := newStubValidator()
	scm := newStubSCM()
	notif := &stubNotifier{autoApprove: true}

	r := NewRunner(plan, impl, val, scm, notif)
	outcome := r.RunTask(context.Background(), 2) // Low/R1 fixture task

	if outcome.Status != "auto_completed" {
		t.Fatalf("outcome = %+v, want auto_completed", outcome)
	}
	if len(scm.commits) != 1 || scm.commits[0] != 2 {
		t.Errorf("expected a commit for task 2, got %v", scm.commits)
	}
	if len(notif.digest) != 1 {
		t.Errorf("expected task 2 queued for the digest, got %v", notif.digest)
	}

	reloaded, err := ParsePlan(plan.Path)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for _, row := range reloaded.Index {
		if row.Task == 2 && !row.Done {
			t.Errorf("task 2 should be marked Done on disk after auto-completion")
		}
	}
}

func TestRunTaskGatedPausesForApproval(t *testing.T) {
	plan := loadFixture(t)
	impl := newStubImplementer()
	val := newStubValidator()
	scm := newStubSCM()
	notif := &stubNotifier{autoApprove: true}

	r := NewRunner(plan, impl, val, scm, notif)
	outcome := r.RunTask(context.Background(), 3) // High/R3 fixture task

	if outcome.Status != "gated_approved" {
		t.Fatalf("outcome = %+v, want gated_approved", outcome)
	}
	if len(notif.gatedCalls) != 1 || notif.gatedCalls[0] != 3 {
		t.Errorf("expected NotifyGated for task 3, got %v", notif.gatedCalls)
	}
	if len(scm.commits) != 1 || scm.commits[0] != 3 {
		t.Errorf("expected commit only after approval, got %v", scm.commits)
	}
}

func TestRunTaskGatedRejected(t *testing.T) {
	plan := loadFixture(t)
	impl := newStubImplementer()
	val := newStubValidator()
	scm := newStubSCM()
	notif := &stubNotifier{autoApprove: false}

	r := NewRunner(plan, impl, val, scm, notif)
	outcome := r.RunTask(context.Background(), 3)

	if outcome.Status != "gated_rejected" {
		t.Fatalf("outcome = %+v, want gated_rejected", outcome)
	}
	if len(scm.commits) != 0 {
		t.Errorf("a rejected task must never be committed, got %v", scm.commits)
	}
}

func TestRunTaskHaltsAfterTwoConsecutiveValidationFailures(t *testing.T) {
	plan := loadFixture(t)
	impl := newStubImplementer()
	val := newStubValidator(4) // task 4 always fails validation
	scm := newStubSCM()
	notif := &stubNotifier{autoApprove: true}

	r := NewRunner(plan, impl, val, scm, notif)
	outcome := r.RunTask(context.Background(), 4)

	if outcome.Status != "halted" {
		t.Fatalf("outcome = %+v, want halted", outcome)
	}
	if val.calls[4] != 2 {
		t.Errorf("expected exactly one retry (2 validate calls), got %d", val.calls[4])
	}
	if len(notif.haltCalls) != 1 || notif.haltCalls[0] != 4 {
		t.Errorf("expected a halt alert for task 4, got %v", notif.haltCalls)
	}
	if len(scm.commits) != 0 {
		t.Errorf("a halted task must never be committed, got %v", scm.commits)
	}

	reloaded, err := ParsePlan(plan.Path)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for _, row := range reloaded.Index {
		if row.Task == 4 && row.Done {
			t.Errorf("a halted task must not be marked Done")
		}
	}
}

func TestRunTaskFrozenSkipsImmediately(t *testing.T) {
	plan := loadFixture(t)
	impl := newStubImplementer()
	val := newStubValidator()
	scm := newStubSCM()
	notif := &stubNotifier{frozen: true}

	r := NewRunner(plan, impl, val, scm, notif)
	outcome := r.RunTask(context.Background(), 2)

	if outcome.Status != "frozen" {
		t.Fatalf("outcome = %+v, want frozen", outcome)
	}
	if impl.implCall[2] != 0 {
		t.Errorf("a frozen runner must never invoke Implement, got %d calls", impl.implCall[2])
	}
}

func TestSelectBatchSkipsDoneAndBlockedRows(t *testing.T) {
	plan := loadFixture(t)
	r := NewRunner(plan, newStubImplementer(), newStubValidator(), newStubSCM(), &stubNotifier{})

	batch := r.SelectBatch()
	if len(batch) != 1 {
		t.Fatalf("batch = %v, want exactly the lowest-numbered eligible non-[P] task", batch)
	}
	if batch[0] != 2 {
		t.Errorf("batch[0] = %d, want 2", batch[0])
	}
}

func TestRunAllStopsOnHalt(t *testing.T) {
	plan := loadFixture(t)
	impl := newStubImplementer()
	val := newStubValidator(4)
	scm := newStubSCM()
	notif := &stubNotifier{autoApprove: true}

	r := NewRunner(plan, impl, val, scm, notif)
	outcomes := r.RunAll(context.Background())

	if len(outcomes) == 0 {
		t.Fatalf("expected at least one outcome")
	}
	last := outcomes[len(outcomes)-1]
	if last.Status != "halted" {
		t.Fatalf("last outcome = %+v, want halted (task 4 always fails)", last)
	}
	// Task 4 sorts after 2 and 3, so both should have completed before the halt.
	if len(scm.commits) != 2 {
		t.Errorf("expected tasks 2 and 3 committed before the halt, got %v", scm.commits)
	}
}
