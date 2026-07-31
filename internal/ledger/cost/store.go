package cost

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// Scope is the aggregation level a Budget or Entry is attributed to,
// shared with cost_entries.scope (00006_ledgers.sql).
type Scope string

const (
	ScopeWorkflow Scope = "workflow"
	ScopeProduct  Scope = "product"
	ScopeMission  Scope = "mission"
	// ScopeSession is the per-session budget scope (docs/PLAN.md Task 119 /
	// COST-01): a single unattended execution session within a mission, so a
	// runaway session cannot consume the whole mission envelope before the
	// mission-level ceiling notices.
	ScopeSession Scope = "session"
)

// Kind enumerates budgets.kind — the envelope categories from
// cost-accounting.md §2's `budgets:` block.
type Kind string

const (
	KindMissionMonthly Kind = "mission_monthly"
	KindProvider       Kind = "provider"
	KindInfra          Kind = "infra"
	KindExperiment     Kind = "experiment"
	KindReserve        Kind = "reserve"
)

// State is one of cost_entries.state's values (00006/00009 migrations),
// mirroring cost-accounting.md §1's "reserved → estimated → incurred →
// reconciled" pipeline plus the 'released' and 'shadow' states 00009 adds.
type State string

const (
	StateReserved   State = "reserved"
	StateEstimated  State = "estimated"
	StateIncurred   State = "incurred"
	StateReconciled State = "reconciled"
	StateReleased   State = "released"
	StateShadow     State = "shadow"
)

// ErrBudgetNotFound is returned when no budgets row matches the requested
// (scope, scope_id, kind, period) envelope key.
var ErrBudgetNotFound = errors.New("cost: budget envelope not found")

// ErrBudgetExists is returned by CreateBudget when the envelope key is
// already provisioned.
var ErrBudgetExists = errors.New("cost: budget envelope already exists")

// ErrBudgetExhausted is returned by Reserve when granting the requested
// amount would exceed the envelope's ceiling — the core C19 enforcement
// outcome, not an infrastructure fault.
var ErrBudgetExhausted = errors.New("cost: reservation would exceed budget ceiling")

// ErrCeilingNotHigher is returned by RaiseCeiling when the requested
// ceiling is not greater than the envelope's current ceiling — "raise" is
// monotonically increasing only.
var ErrCeilingNotHigher = errors.New("cost: new ceiling is not higher than the current ceiling")

// ErrEntryNotFound is returned when no cost_entries row matches the
// requested id.
var ErrEntryNotFound = errors.New("cost: entry not found")

// ErrNotReserved is returned by Incur/Release when the target entry is not
// currently in the reserved state.
var ErrNotReserved = errors.New("cost: entry is not in the reserved state")

// ErrNotIncurred is returned by Reconcile when the target entry is not
// currently in the incurred state.
var ErrNotIncurred = errors.New("cost: entry is not in the incurred state")

// Budget is one budgets row: an envelope ceiling and its running totals.
type Budget struct {
	ID          string
	Scope       Scope
	ScopeID     string
	Kind        Kind
	Period      string
	CeilingUSD  float64
	ReservedUSD float64
	IncurredUSD float64
}

// Entry is one cost_entries row.
type Entry struct {
	ID             string
	Scope          Scope
	ScopeID        string
	State          State
	AmountUSD      float64
	PricingVersion string
	Provider       string
	BudgetID       string
	Meta           json.RawMessage
	At             time.Time
}

// Store is the Postgres-backed cost ledger
// (internal/db/migrations/00006_ledgers.sql + 00009_budgets.sql).
type Store struct {
	db *sql.DB
}

// NewStore wraps an existing *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// nonFiniteUSD reports whether v is NaN or +/-Inf. Unlike IEEE 754 double
// precision, PostgreSQL's NUMERIC type accepts and orders both NaN and
// Infinity as legal values — and treats NaN/+Inf as greater than every
// finite value. That means a NaN or Infinity ceiling_usd would make
// Reserve's `ceiling_usd - (reserved_usd + incurred_usd) >= $amount` WHERE
// clause true for any finite amount, silently defeating the envelope's
// cap entirely; and RaiseCeiling's monotonic `ceiling_usd < $new` guard
// would let a NaN/Inf value through as if it were "higher" than the
// current ceiling. This check is what stands between a malformed
// ceiling/amount (e.g. a CLI flag parsed with strconv.ParseFloat, which
// happily accepts "NaN"/"Inf") and that bypass, so every USD amount this
// package writes to budgets.ceiling_usd or reserves must pass it first.
func nonFiniteUSD(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}

func newID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cost: generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
}

// CreateBudget provisions a new envelope. It fails with ErrBudgetExists if
// the (scope, scope_id, kind, period) key is already provisioned — callers
// that want "provision or reuse" should check that error and fall back to
// GetBudget.
func (s *Store) CreateBudget(ctx context.Context, scope Scope, scopeID string, kind Kind, period string, ceilingUSD float64) (Budget, error) {
	if nonFiniteUSD(ceilingUSD) {
		return Budget{}, fmt.Errorf("cost: create budget %s/%s/%s/%s: ceiling_usd must be finite, got %v", scope, scopeID, kind, period, ceilingUSD)
	}
	id, err := newID("budget")
	if err != nil {
		return Budget{}, err
	}

	const insert = `
INSERT INTO budgets (id, scope, scope_id, kind, period, ceiling_usd)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (scope, scope_id, kind, period) DO NOTHING
RETURNING id, scope, scope_id, kind, period, ceiling_usd, reserved_usd, incurred_usd`

	budget, err := scanBudget(s.db.QueryRowContext(ctx, insert, id, scope, scopeID, kind, period, ceilingUSD))
	if err == nil {
		return budget, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Budget{}, ErrBudgetExists
	}
	return Budget{}, fmt.Errorf("cost: create budget %s/%s/%s/%s: %w", scope, scopeID, kind, period, err)
}

// GetBudget loads one envelope by its (scope, scope_id, kind, period) key.
func (s *Store) GetBudget(ctx context.Context, scope Scope, scopeID string, kind Kind, period string) (Budget, error) {
	const q = `
SELECT id, scope, scope_id, kind, period, ceiling_usd, reserved_usd, incurred_usd
FROM budgets WHERE scope = $1 AND scope_id = $2 AND kind = $3 AND period = $4`
	budget, err := scanBudget(s.db.QueryRowContext(ctx, q, scope, scopeID, kind, period))
	if errors.Is(err, sql.ErrNoRows) {
		return Budget{}, ErrBudgetNotFound
	}
	if err != nil {
		return Budget{}, fmt.Errorf("cost: get budget %s/%s/%s/%s: %w", scope, scopeID, kind, period, err)
	}
	return budget, nil
}

// ListBudgets returns every envelope provisioned for (scope, scope_id),
// ordered by kind then period — the read path `foundry cost show` uses.
func (s *Store) ListBudgets(ctx context.Context, scope Scope, scopeID string) ([]Budget, error) {
	const q = `
SELECT id, scope, scope_id, kind, period, ceiling_usd, reserved_usd, incurred_usd
FROM budgets WHERE scope = $1 AND scope_id = $2 ORDER BY kind, period`
	rows, err := s.db.QueryContext(ctx, q, scope, scopeID)
	if err != nil {
		return nil, fmt.Errorf("cost: list budgets %s/%s: %w", scope, scopeID, err)
	}
	defer func() { _ = rows.Close() }()

	var budgets []Budget
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, fmt.Errorf("cost: scan budget %s/%s: %w", scope, scopeID, err)
		}
		budgets = append(budgets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost: list budgets %s/%s: %w", scope, scopeID, err)
	}
	return budgets, nil
}

// RaiseCeiling sets an envelope's ceiling_usd to newCeilingUSD, provided
// that is strictly greater than the current ceiling ("raise" is
// monotonically increasing only — lowering a ceiling is a distinct,
// out-of-scope operation this task's card never asks for). This is the
// operation `foundry budget raise` calls; the CLI command is responsible
// for the audited side of the operation (internal/provenance's
// AppendAuditRow) — this method only ever touches the budgets table.
func (s *Store) RaiseCeiling(ctx context.Context, scope Scope, scopeID string, kind Kind, period string, newCeilingUSD float64) (Budget, error) {
	if nonFiniteUSD(newCeilingUSD) {
		return Budget{}, fmt.Errorf("cost: raise ceiling %s/%s/%s/%s: new ceiling_usd must be finite, got %v", scope, scopeID, kind, period, newCeilingUSD)
	}
	const update = `
UPDATE budgets
SET ceiling_usd = $5, updated_at = now()
WHERE scope = $1 AND scope_id = $2 AND kind = $3 AND period = $4 AND ceiling_usd < $5
RETURNING id, scope, scope_id, kind, period, ceiling_usd, reserved_usd, incurred_usd`

	budget, err := scanBudget(s.db.QueryRowContext(ctx, update, scope, scopeID, kind, period, newCeilingUSD))
	if err == nil {
		return budget, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Budget{}, fmt.Errorf("cost: raise ceiling %s/%s/%s/%s: %w", scope, scopeID, kind, period, err)
	}

	// Zero rows updated: distinguish "no such envelope" from "not actually
	// higher" so callers get an actionable error.
	if _, getErr := s.GetBudget(ctx, scope, scopeID, kind, period); errors.Is(getErr, ErrBudgetNotFound) {
		return Budget{}, ErrBudgetNotFound
	}
	return Budget{}, ErrCeilingNotHigher
}

// Reserve atomically reserves amountUSD against the (scope, scope_id,
// kind, period) envelope and records the reservation as a new 'reserved'
// cost_entries row, all inside one transaction. See doc.go for why the
// budgets UPDATE this runs inside that transaction is race-free under
// real concurrency: the row lock the UPDATE takes is held until the
// transaction commits, so no two concurrent Reserve calls against the
// same envelope ever both observe the same "amount available" and both
// proceed.
func (s *Store) Reserve(ctx context.Context, scope Scope, scopeID string, kind Kind, period string, amountUSD float64, provider, pricingVersion string, meta any) (Entry, error) {
	if amountUSD <= 0 || nonFiniteUSD(amountUSD) {
		return Entry{}, fmt.Errorf("cost: reserve amount must be a positive finite number, got %v", amountUSD)
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return Entry{}, fmt.Errorf("cost: marshal meta: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("cost: reserve %s/%s/%s/%s: begin tx: %w", scope, scopeID, kind, period, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	const updateBudget = `
UPDATE budgets
SET reserved_usd = reserved_usd + $5, updated_at = now()
WHERE scope = $1 AND scope_id = $2 AND kind = $3 AND period = $4
  AND ceiling_usd - (reserved_usd + incurred_usd) >= $5
RETURNING id`

	var budgetID string
	err = tx.QueryRowContext(ctx, updateBudget, scope, scopeID, kind, period, amountUSD).Scan(&budgetID)
	if errors.Is(err, sql.ErrNoRows) {
		// Zero rows updated: either no such envelope, or it exists but
		// cannot cover amountUSD. A plain SELECT here is safe (not a
		// second half of a check-then-act race) because it only decides
		// which error to return — no reservation is made on either path.
		if _, getErr := s.GetBudget(ctx, scope, scopeID, kind, period); errors.Is(getErr, ErrBudgetNotFound) {
			return Entry{}, ErrBudgetNotFound
		}
		return Entry{}, ErrBudgetExhausted
	}
	if err != nil {
		return Entry{}, fmt.Errorf("cost: reserve %s/%s/%s/%s: %w", scope, scopeID, kind, period, err)
	}

	id, err := newID("entry")
	if err != nil {
		return Entry{}, err
	}

	const insertEntry = `
INSERT INTO cost_entries (id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id)
VALUES ($1, $2, $3, 'reserved', $4, $5, $6, $7, $8)
RETURNING id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at`

	entry, err := scanEntry(tx.QueryRowContext(ctx, insertEntry, id, scope, scopeID, amountUSD, pricingVersion, provider, payload, budgetID))
	if err != nil {
		return Entry{}, fmt.Errorf("cost: reserve %s/%s/%s/%s: insert entry: %w", scope, scopeID, kind, period, err)
	}

	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("cost: reserve %s/%s/%s/%s: commit: %w", scope, scopeID, kind, period, err)
	}
	return entry, nil
}

// Incur transitions a reserved entry to incurred, replacing its reserved
// estimate with actualAmountUSD and moving that amount from the envelope's
// reserved_usd bucket into incurred_usd (so the envelope's available
// amount reflects real spend rather than the original estimate).
func (s *Store) Incur(ctx context.Context, entryID string, actualAmountUSD float64) (Entry, error) {
	if nonFiniteUSD(actualAmountUSD) {
		return Entry{}, fmt.Errorf("cost: incur %s: actual amount must be finite, got %v", entryID, actualAmountUSD)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("cost: incur %s: begin tx: %w", entryID, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	const selectForUpdate = `
SELECT id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at
FROM cost_entries WHERE id = $1 FOR UPDATE`
	entry, err := scanEntry(tx.QueryRowContext(ctx, selectForUpdate, entryID))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrEntryNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("cost: incur %s: %w", entryID, err)
	}
	if entry.State != StateReserved {
		return Entry{}, ErrNotReserved
	}

	if entry.BudgetID != "" {
		const updateBudget = `
UPDATE budgets
SET reserved_usd = reserved_usd - $2, incurred_usd = incurred_usd + $3, updated_at = now()
WHERE id = $1`
		if _, err := tx.ExecContext(ctx, updateBudget, entry.BudgetID, entry.AmountUSD, actualAmountUSD); err != nil {
			return Entry{}, fmt.Errorf("cost: incur %s: update budget: %w", entryID, err)
		}
	}

	const updateEntry = `
UPDATE cost_entries SET state = 'incurred', amount_usd = $2 WHERE id = $1
RETURNING id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at`
	updated, err := scanEntry(tx.QueryRowContext(ctx, updateEntry, entryID, actualAmountUSD))
	if err != nil {
		return Entry{}, fmt.Errorf("cost: incur %s: update entry: %w", entryID, err)
	}

	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("cost: incur %s: commit: %w", entryID, err)
	}
	return updated, nil
}

// Reconcile compares observedAmountUSD against an incurred entry's
// recorded amount, transitions it to reconciled, and adjusts the
// envelope's incurred_usd by the difference so the aggregate reflects the
// reconciled (final) amount rather than the incurred estimate. diverged
// reports whether observedAmountUSD differed from the amount recorded at
// Incur time.
func (s *Store) Reconcile(ctx context.Context, entryID string, observedAmountUSD float64) (diverged bool, err error) {
	if nonFiniteUSD(observedAmountUSD) {
		return false, fmt.Errorf("cost: reconcile %s: observed amount must be finite, got %v", entryID, observedAmountUSD)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("cost: reconcile %s: begin tx: %w", entryID, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	const selectForUpdate = `
SELECT id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at
FROM cost_entries WHERE id = $1 FOR UPDATE`
	entry, err := scanEntry(tx.QueryRowContext(ctx, selectForUpdate, entryID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrEntryNotFound
	}
	if err != nil {
		return false, fmt.Errorf("cost: reconcile %s: %w", entryID, err)
	}
	if entry.State != StateIncurred {
		return false, ErrNotIncurred
	}

	delta := observedAmountUSD - entry.AmountUSD
	if entry.BudgetID != "" && delta != 0 {
		const updateBudget = `UPDATE budgets SET incurred_usd = incurred_usd + $2, updated_at = now() WHERE id = $1`
		if _, err := tx.ExecContext(ctx, updateBudget, entry.BudgetID, delta); err != nil {
			return false, fmt.Errorf("cost: reconcile %s: update budget: %w", entryID, err)
		}
	}

	const updateEntry = `UPDATE cost_entries SET state = 'reconciled', amount_usd = $2 WHERE id = $1`
	if _, err := tx.ExecContext(ctx, updateEntry, entryID, observedAmountUSD); err != nil {
		return false, fmt.Errorf("cost: reconcile %s: update entry: %w", entryID, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("cost: reconcile %s: commit: %w", entryID, err)
	}
	return delta != 0, nil
}

// Release returns an unspent reservation to its envelope: the entry
// transitions reserved -> released and its amount is subtracted back out
// of the envelope's reserved_usd, freeing that headroom for other
// reservations. Only a currently-reserved entry may be released.
func (s *Store) Release(ctx context.Context, entryID string) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("cost: release %s: begin tx: %w", entryID, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	const selectForUpdate = `
SELECT id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at
FROM cost_entries WHERE id = $1 FOR UPDATE`
	entry, err := scanEntry(tx.QueryRowContext(ctx, selectForUpdate, entryID))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrEntryNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("cost: release %s: %w", entryID, err)
	}
	if entry.State != StateReserved {
		return Entry{}, ErrNotReserved
	}

	if entry.BudgetID != "" {
		const updateBudget = `UPDATE budgets SET reserved_usd = reserved_usd - $2, updated_at = now() WHERE id = $1`
		if _, err := tx.ExecContext(ctx, updateBudget, entry.BudgetID, entry.AmountUSD); err != nil {
			return Entry{}, fmt.Errorf("cost: release %s: update budget: %w", entryID, err)
		}
	}

	const updateEntry = `
UPDATE cost_entries SET state = 'released' WHERE id = $1
RETURNING id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at`
	updated, err := scanEntry(tx.QueryRowContext(ctx, updateEntry, entryID))
	if err != nil {
		return Entry{}, fmt.Errorf("cost: release %s: update entry: %w", entryID, err)
	}

	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("cost: release %s: commit: %w", entryID, err)
	}
	return updated, nil
}

// RecordShadow records a subscription-priced cost entry with no real
// reservation and no ceiling check (cost-accounting.md §1: "shadow
// (subscription usage priced at equivalent API rates)") — for executors
// billed as a flat-rate subscription rather than metered per call, there
// is no real per-call price to reserve against, so this is an
// observability-only record, not a spend-blocking one.
func (s *Store) RecordShadow(ctx context.Context, scope Scope, scopeID string, amountUSD float64, provider, pricingVersion string, meta any) (Entry, error) {
	if nonFiniteUSD(amountUSD) {
		return Entry{}, fmt.Errorf("cost: record shadow %s/%s: amount must be finite, got %v", scope, scopeID, amountUSD)
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return Entry{}, fmt.Errorf("cost: marshal meta: %w", err)
	}
	id, err := newID("entry")
	if err != nil {
		return Entry{}, err
	}

	const insert = `
INSERT INTO cost_entries (id, scope, scope_id, state, amount_usd, pricing_version, provider, meta)
VALUES ($1, $2, $3, 'shadow', $4, $5, $6, $7)
RETURNING id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at`

	entry, err := scanEntry(s.db.QueryRowContext(ctx, insert, id, scope, scopeID, amountUSD, pricingVersion, provider, payload))
	if err != nil {
		return Entry{}, fmt.Errorf("cost: record shadow %s/%s: %w", scope, scopeID, err)
	}
	return entry, nil
}

// GetEntry loads one cost_entries row by id.
func (s *Store) GetEntry(ctx context.Context, entryID string) (Entry, error) {
	const q = `
SELECT id, scope, scope_id, state, amount_usd, pricing_version, provider, meta, budget_id, at
FROM cost_entries WHERE id = $1`
	entry, err := scanEntry(s.db.QueryRowContext(ctx, q, entryID))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrEntryNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("cost: get entry %s: %w", entryID, err)
	}
	return entry, nil
}

// rowScanner is the common subset of *sql.Row and *sql.Rows used below.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanBudget(row rowScanner) (Budget, error) {
	var b Budget
	var scope, kind string
	if err := row.Scan(&b.ID, &scope, &b.ScopeID, &kind, &b.Period, &b.CeilingUSD, &b.ReservedUSD, &b.IncurredUSD); err != nil {
		return Budget{}, err
	}
	b.Scope = Scope(scope)
	b.Kind = Kind(kind)
	return b, nil
}

func scanEntry(row rowScanner) (Entry, error) {
	var e Entry
	var scope, state string
	var meta []byte
	var budgetID sql.NullString
	if err := row.Scan(&e.ID, &scope, &e.ScopeID, &state, &e.AmountUSD, &e.PricingVersion, &e.Provider, &meta, &budgetID, &e.At); err != nil {
		return Entry{}, err
	}
	e.Scope = Scope(scope)
	e.State = State(state)
	e.Meta = meta
	e.BudgetID = budgetID.String
	return e, nil
}
