package kernel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

// SignalBudgetRaised is the Temporal signal name DeliverPlan listens on
// while paused WAITING/budget (docs/PLAN.md Task 29/FND-10, Constitution
// C19) — `foundry budget raise` sends this signal after raising the
// relevant envelope's ceiling so a paused workflow resumes without a
// restart.
const SignalBudgetRaised = "budget-raised"

// costPricingVersion is a placeholder pricing-catalog version stamped on
// every cost_entries row this package writes. decision (no-gaps rule):
// this task's card does not ask for a real pricing-version registry (a
// mapping from provider/model to $/token); a future task can add one and
// thread a real version through ReserveBudgetInput without changing this
// package's contract.
const costPricingVersion = "v1"

// subscriptionExecutors names executor.Adapter implementations billed as a
// flat-rate subscription rather than metered per call
// (cost-accounting.md §1: "shadow (subscription usage priced at
// equivalent API rates)"). A reservation against one of these has no real
// per-call price to check a ceiling against, so ReserveBudget records a
// shadow cost.Entry instead of a real reservation.
var subscriptionExecutors = map[string]bool{
	"claudecode": true,
}

func isSubscriptionExecutor(name string) bool {
	return subscriptionExecutors[name]
}

// currentPeriod returns the calendar-month bucket (YYYY-MM) for at, used
// as budgets.period for the mission_monthly envelope kind.
func currentPeriod(at time.Time) string {
	return at.Format("2006-01")
}

// BudgetStore is the subset of internal/ledger/cost.Store's behavior the
// kernel's ReserveBudget activity depends on (interfaces defined in the
// consuming package — the same pattern LeaseStore/ReceiptStore/
// TransitionStore already use in this package's sibling files).
// *cost.Store satisfies this structurally; MemBudgetStore below is the
// in-memory fake for tests that don't need a live Postgres.
type BudgetStore interface {
	// Reserve atomically reserves amountUSD against the (scope, scope_id,
	// kind, period) envelope. Returns cost.ErrBudgetNotFound if no
	// envelope is provisioned for that key, or cost.ErrBudgetExhausted if
	// granting amountUSD would exceed the envelope's ceiling.
	Reserve(ctx context.Context, scope cost.Scope, scopeID string, kind cost.Kind, period string, amountUSD float64, provider, pricingVersion string, meta any) (cost.Entry, error)
	// RecordShadow records a subscription-priced entry with no ceiling
	// check at all (cost-accounting.md §1).
	RecordShadow(ctx context.Context, scope cost.Scope, scopeID string, amountUSD float64, provider, pricingVersion string, meta any) (cost.Entry, error)
	// Incur transitions a reserved entry to incurred with the real observed
	// amount (docs/PLAN.md Task 120 / COST-02): the reserve→incur half of the
	// cost ledger.
	Incur(ctx context.Context, entryID string, actualAmountUSD float64) (cost.Entry, error)
}

// budgetKey identifies one in-memory envelope for MemBudgetStore.
type budgetKey struct {
	scope   cost.Scope
	scopeID string
	kind    cost.Kind
	period  string
}

// MemBudgetStore is an in-memory BudgetStore for kernel workflow tests. It
// deliberately does not reproduce internal/ledger/cost.Store's Postgres
// row-locking mechanism — that atomicity property is proven directly
// against a real Postgres by internal/ledger/cost's own concurrency
// property test (docs/PLAN.md Task 29 Acceptance). This fake exists only
// to let kernel tests exercise the WAITING/budget wiring deterministically
// via go.temporal.io/sdk/testsuite, which never runs two activities of the
// same workflow concurrently in the first place.
type MemBudgetStore struct {
	incurred map[string]float64
	mu       sync.Mutex
	ceiling  map[budgetKey]float64
	spent    map[budgetKey]float64
	seq      int
}

// NewMemBudgetStore returns a MemBudgetStore with no envelopes configured —
// every Reserve call is unmetered (mirrors cost.Store's ErrBudgetNotFound
// behavior) until SetCeiling provisions a key.
func NewMemBudgetStore() *MemBudgetStore {
	return &MemBudgetStore{ceiling: make(map[budgetKey]float64), spent: make(map[budgetKey]float64)}
}

// SetCeiling provisions (or replaces) the ceiling for one envelope key.
func (s *MemBudgetStore) SetCeiling(scope cost.Scope, scopeID string, kind cost.Kind, period string, ceilingUSD float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ceiling[budgetKey{scope, scopeID, kind, period}] = ceilingUSD
}

// Reserve implements BudgetStore.
func (s *MemBudgetStore) Reserve(_ context.Context, scope cost.Scope, scopeID string, kind cost.Kind, period string, amountUSD float64, provider, pricingVersion string, _ any) (cost.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := budgetKey{scope, scopeID, kind, period}
	ceiling, ok := s.ceiling[key]
	if !ok {
		return cost.Entry{}, cost.ErrBudgetNotFound
	}
	if s.spent[key]+amountUSD > ceiling {
		return cost.Entry{}, cost.ErrBudgetExhausted
	}
	s.spent[key] += amountUSD
	s.seq++
	return cost.Entry{
		ID:             fmt.Sprintf("mem-entry-%d", s.seq),
		Scope:          scope,
		ScopeID:        scopeID,
		State:          cost.StateReserved,
		AmountUSD:      amountUSD,
		Provider:       provider,
		PricingVersion: pricingVersion,
	}, nil
}

// RecordShadow implements BudgetStore.
func (s *MemBudgetStore) RecordShadow(_ context.Context, scope cost.Scope, scopeID string, amountUSD float64, provider, pricingVersion string, _ any) (cost.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return cost.Entry{
		ID:             fmt.Sprintf("mem-shadow-%d", s.seq),
		Scope:          scope,
		ScopeID:        scopeID,
		State:          cost.StateShadow,
		AmountUSD:      amountUSD,
		Provider:       provider,
		PricingVersion: pricingVersion,
	}, nil
}

// Incur implements BudgetStore for MemBudgetStore: it records the real amount as
// incurred (Task 120). The in-memory store tracks incurred totals per entry so
// tests can assert reserve→incur without a live Postgres.
func (s *MemBudgetStore) Incur(_ context.Context, entryID string, actualAmountUSD float64) (cost.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.incurred == nil {
		s.incurred = map[string]float64{}
	}
	s.incurred[entryID] = actualAmountUSD
	return cost.Entry{ID: entryID, State: cost.StateIncurred, AmountUSD: actualAmountUSD}, nil
}

// IncurredFor returns the incurred amount recorded for an entry (test helper).
func (s *MemBudgetStore) IncurredFor(entryID string) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.incurred[entryID]
	return v, ok
}
