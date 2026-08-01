package mission

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Portfolio-store sentinels so a transport (workflow/CLI) can branch on the
// outcome without string-matching.
var (
	// ErrPortfolioNotFound is returned when no portfolio_state row exists.
	ErrPortfolioNotFound = errors.New("mission: portfolio not found")
	// ErrPortfolioMissionNotFound is returned when a mission is not in the
	// portfolio.
	ErrPortfolioMissionNotFound = errors.New("mission: mission not in portfolio")
	// ErrPortfolioCapReached is returned when an activation would exceed the
	// active-mission cap. Fail-closed: the cap holds across a restart because
	// activation is checked against the persisted, row-locked state.
	ErrPortfolioCapReached = errors.New("mission: portfolio active-mission cap reached")
	// ErrPortfolioOverBudget is returned when a charge would exceed a
	// mission's OWN monthly envelope. It never touches any other mission.
	ErrPortfolioOverBudget = errors.New("mission: charge exceeds mission monthly budget")
)

// PortfolioStore persists all mutable portfolio state in Postgres so the
// active-mission cap, per-mission budget isolation and the fairness bound
// survive a foundryd restart instead of resetting to zero (docs/PLAN.md Task
// 121). Every state mutation that could race the cap serializes on the
// portfolio_state row via SELECT ... FOR UPDATE and bumps its version, so two
// workers can never both activate past the cap.
type PortfolioStore struct {
	db *sql.DB
}

// NewPortfolioStore constructs a PortfolioStore over db.
func NewPortfolioStore(db *sql.DB) *PortfolioStore { return &PortfolioStore{db: db} }

// EnsurePortfolio creates the portfolio_state row for portfolioID with the
// given active-mission cap if it does not already exist. It never changes an
// existing cap (Task 121 Out-of-scope: quota numbers are not this card's to
// change), so calling it on every startup is idempotent.
func (s *PortfolioStore) EnsurePortfolio(ctx context.Context, portfolioID string, maxActive int) error {
	if portfolioID == "" {
		return fmt.Errorf("mission: portfolio requires an ID")
	}
	if maxActive < 0 {
		return fmt.Errorf("mission: portfolio cap must be non-negative, got %d", maxActive)
	}
	const q = `
INSERT INTO portfolio_state (portfolio_id, max_active_products)
VALUES ($1, $2)
ON CONFLICT (portfolio_id) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, q, portfolioID, maxActive); err != nil {
		return fmt.Errorf("mission: ensure portfolio %q: %w", portfolioID, err)
	}
	return nil
}

// UpsertMission registers m into portfolioID idempotently. A mission already
// present keeps its persisted activation/spend/schedule (a restart must not
// reset them); only budget metadata and revenue flag are refreshed.
func (s *PortfolioStore) UpsertMission(ctx context.Context, portfolioID string, m PortfolioMission) error {
	if m.ID == "" {
		return fmt.Errorf("mission: portfolio mission requires an ID")
	}
	scope := m.BudgetScope
	if scope == "" {
		scope = m.ID
	}
	const q = `
INSERT INTO portfolio_schedule
    (portfolio_id, mission_id, active, revenue_bearing, monthly_budget_usd, spent_usd, budget_scope)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (portfolio_id, mission_id) DO UPDATE
    SET revenue_bearing = EXCLUDED.revenue_bearing,
        monthly_budget_usd = EXCLUDED.monthly_budget_usd,
        budget_scope = EXCLUDED.budget_scope,
        updated_at = now()`
	if _, err := s.db.ExecContext(ctx, q, portfolioID, m.ID, m.Active, m.RevenueBearing, m.MonthlyBudgetUSD, m.SpentUSD, scope); err != nil {
		return fmt.Errorf("mission: upsert portfolio mission %q/%q: %w", portfolioID, m.ID, err)
	}
	return nil
}

// Activate marks missionID active, failing CLOSED with ErrPortfolioCapReached
// if that would exceed the cap. It serializes on the portfolio_state row so a
// concurrent activation on the same portfolio cannot also pass the cap.
func (s *PortfolioStore) Activate(ctx context.Context, portfolioID, missionID string) error {
	return s.mutateActive(ctx, portfolioID, missionID, true)
}

// Deactivate marks missionID inactive.
func (s *PortfolioStore) Deactivate(ctx context.Context, portfolioID, missionID string) error {
	return s.mutateActive(ctx, portfolioID, missionID, false)
}

func (s *PortfolioStore) mutateActive(ctx context.Context, portfolioID, missionID string, active bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mission: portfolio activate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// FOR UPDATE serializes concurrent activation attempts on this portfolio:
	// the second worker blocks here until the first commits, then observes the
	// updated active count and fails closed if the cap is now full.
	var cap int
	err = tx.QueryRowContext(ctx,
		`SELECT max_active_products FROM portfolio_state WHERE portfolio_id = $1 FOR UPDATE`,
		portfolioID).Scan(&cap)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPortfolioNotFound
	}
	if err != nil {
		return fmt.Errorf("mission: portfolio activate: lock state: %w", err)
	}

	var curActive bool
	err = tx.QueryRowContext(ctx,
		`SELECT active FROM portfolio_schedule WHERE portfolio_id = $1 AND mission_id = $2`,
		portfolioID, missionID).Scan(&curActive)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPortfolioMissionNotFound
	}
	if err != nil {
		return fmt.Errorf("mission: portfolio activate: read mission: %w", err)
	}
	if curActive == active {
		return nil // idempotent
	}

	if active && cap > 0 {
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM portfolio_schedule WHERE portfolio_id = $1 AND active`,
			portfolioID).Scan(&n); err != nil {
			return fmt.Errorf("mission: portfolio activate: count active: %w", err)
		}
		if n >= cap {
			return fmt.Errorf("%w: %q (cap %d)", ErrPortfolioCapReached, missionID, cap)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE portfolio_schedule SET active = $3, updated_at = now() WHERE portfolio_id = $1 AND mission_id = $2`,
		portfolioID, missionID, active); err != nil {
		return fmt.Errorf("mission: portfolio activate: update mission: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE portfolio_state SET version = version + 1, updated_at = now() WHERE portfolio_id = $1`,
		portfolioID); err != nil {
		return fmt.Errorf("mission: portfolio activate: bump version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mission: portfolio activate: commit: %w", err)
	}
	return nil
}

// Charge spends amount against missionID's OWN envelope only. The single
// conditional UPDATE touches exactly one row, so it can never draw down another
// mission's spent_usd -- budget bleed between missions is impossible by
// construction, and the same isolation is provable at the cost-ledger layer
// (see PortfolioStore-backed tests querying cost_entries/budgets).
func (s *PortfolioStore) Charge(ctx context.Context, portfolioID, missionID string, amount float64) error {
	if amount < 0 {
		return fmt.Errorf("mission: negative charge for %q", missionID)
	}
	const q = `
UPDATE portfolio_schedule
SET spent_usd = spent_usd + $3, updated_at = now()
WHERE portfolio_id = $1 AND mission_id = $2
  AND (monthly_budget_usd = 0 OR spent_usd + $3 <= monthly_budget_usd)
RETURNING mission_id`
	var got string
	err := s.db.QueryRowContext(ctx, q, portfolioID, missionID, amount).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		// Either the mission does not exist or the charge would exceed its own
		// envelope. A plain read decides which error to return; it is safe
		// because no charge was applied on either path.
		var exists bool
		if e := s.db.QueryRowContext(ctx,
			`SELECT true FROM portfolio_schedule WHERE portfolio_id = $1 AND mission_id = $2`,
			portfolioID, missionID).Scan(&exists); errors.Is(e, sql.ErrNoRows) {
			return ErrPortfolioMissionNotFound
		}
		return fmt.Errorf("%w: %q", ErrPortfolioOverBudget, missionID)
	}
	if err != nil {
		return fmt.Errorf("mission: portfolio charge %q/%q: %w", portfolioID, missionID, err)
	}
	return nil
}

// NextScheduled picks the active mission scheduled the fewest times
// (deterministic mission-ID tie-break), increments its persisted counter and
// stamps last_scheduled_at = now, returning the picked mission ID. The
// increment is persisted, so the fairness spread bound holds across a restart,
// not just within one process lifetime. now is passed in (from
// workflow.Now(ctx) on the workflow path) so a replay is reproducible.
func (s *PortfolioStore) NextScheduled(ctx context.Context, portfolioID string, now time.Time) (string, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, fmt.Errorf("mission: portfolio schedule: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// FOR UPDATE serializes scheduling on this portfolio so two workers cannot
	// both pick (and both increment) the same least-scheduled mission.
	if _, err := tx.ExecContext(ctx,
		`SELECT portfolio_id FROM portfolio_state WHERE portfolio_id = $1 FOR UPDATE`,
		portfolioID); errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrPortfolioNotFound
	} else if err != nil {
		return "", false, fmt.Errorf("mission: portfolio schedule: lock state: %w", err)
	}

	var pick string
	err = tx.QueryRowContext(ctx, `
SELECT mission_id FROM portfolio_schedule
WHERE portfolio_id = $1 AND active
ORDER BY scheduled ASC, mission_id ASC
LIMIT 1`, portfolioID).Scan(&pick)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil // no active mission to schedule
	}
	if err != nil {
		return "", false, fmt.Errorf("mission: portfolio schedule: pick: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE portfolio_schedule SET scheduled = scheduled + 1, last_scheduled_at = $3, updated_at = now()
         WHERE portfolio_id = $1 AND mission_id = $2`,
		portfolioID, pick, now.UTC()); err != nil {
		return "", false, fmt.Errorf("mission: portfolio schedule: increment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("mission: portfolio schedule: commit: %w", err)
	}
	return pick, true, nil
}

// Load reconstructs an in-memory Portfolio from the persisted rows, for pure
// computations (fairness spread, panel, digest). It is the read model behind
// `foundry portfolio show|list` and the digest panel.
func (s *PortfolioStore) Load(ctx context.Context, portfolioID string) (*Portfolio, error) {
	var cap int
	err := s.db.QueryRowContext(ctx,
		`SELECT max_active_products FROM portfolio_state WHERE portfolio_id = $1`,
		portfolioID).Scan(&cap)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPortfolioNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mission: load portfolio %q: %w", portfolioID, err)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT mission_id, active, revenue_bearing, monthly_budget_usd, spent_usd, scheduled, last_scheduled_at, budget_scope
FROM portfolio_schedule
WHERE portfolio_id = $1
ORDER BY mission_id ASC`, portfolioID)
	if err != nil {
		return nil, fmt.Errorf("mission: load portfolio %q missions: %w", portfolioID, err)
	}
	defer func() { _ = rows.Close() }()

	p := NewPortfolio(cap)
	for rows.Next() {
		var (
			m       PortfolioMission
			lastSch sql.NullTime
		)
		if err := rows.Scan(&m.ID, &m.Active, &m.RevenueBearing, &m.MonthlyBudgetUSD, &m.SpentUSD, &m.scheduled, &lastSch, &m.BudgetScope); err != nil {
			return nil, fmt.Errorf("mission: load portfolio %q: scan: %w", portfolioID, err)
		}
		if lastSch.Valid {
			t := lastSch.Time
			m.LastScheduledAt = &t
		}
		cp := m
		p.missions[m.ID] = &cp
		p.order = append(p.order, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mission: load portfolio %q: rows: %w", portfolioID, err)
	}
	sort.Strings(p.order)
	return p, nil
}

// ActivatePendingUpToCap activates registered-but-inactive missions in
// deterministic mission-ID order until the active-mission cap is reached,
// returning the IDs it newly activated. It is the supervisor's admission step:
// it never exceeds the cap (fail-closed) and is safe to call every iteration
// because an already-full portfolio activates nothing. The whole scan runs
// under the portfolio_state FOR UPDATE lock so a concurrent supervisor cannot
// race it past the cap.
func (s *PortfolioStore) ActivatePendingUpToCap(ctx context.Context, portfolioID string) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mission: portfolio admit: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	var cap int
	err = tx.QueryRowContext(ctx,
		`SELECT max_active_products FROM portfolio_state WHERE portfolio_id = $1 FOR UPDATE`,
		portfolioID).Scan(&cap)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPortfolioNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mission: portfolio admit: lock state: %w", err)
	}

	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM portfolio_schedule WHERE portfolio_id = $1 AND active`,
		portfolioID).Scan(&active); err != nil {
		return nil, fmt.Errorf("mission: portfolio admit: count active: %w", err)
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT mission_id FROM portfolio_schedule WHERE portfolio_id = $1 AND NOT active ORDER BY mission_id ASC`,
		portfolioID)
	if err != nil {
		return nil, fmt.Errorf("mission: portfolio admit: list pending: %w", err)
	}
	var pending []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("mission: portfolio admit: scan: %w", err)
		}
		pending = append(pending, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mission: portfolio admit: rows: %w", err)
	}

	var activated []string
	for _, id := range pending {
		if cap > 0 && active >= cap {
			break
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE portfolio_schedule SET active = true, updated_at = now() WHERE portfolio_id = $1 AND mission_id = $2`,
			portfolioID, id); err != nil {
			return nil, fmt.Errorf("mission: portfolio admit: activate %q: %w", id, err)
		}
		activated = append(activated, id)
		active++
	}
	if len(activated) > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE portfolio_state SET version = version + 1, updated_at = now() WHERE portfolio_id = $1`,
			portfolioID); err != nil {
			return nil, fmt.Errorf("mission: portfolio admit: bump version: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("mission: portfolio admit: commit: %w", err)
	}
	return activated, nil
}

// ActiveMissionIDs returns the active mission IDs in deterministic order.
func (s *PortfolioStore) ActiveMissionIDs(ctx context.Context, portfolioID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mission_id FROM portfolio_schedule WHERE portfolio_id = $1 AND active ORDER BY mission_id ASC`,
		portfolioID)
	if err != nil {
		return nil, fmt.Errorf("mission: active missions %q: %w", portfolioID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("mission: active missions %q: scan: %w", portfolioID, err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
