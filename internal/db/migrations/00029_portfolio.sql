-- +goose Up
-- Task 121 (MMR-01): durable portfolio scheduler state. internal/mission/
-- portfolio.go was entirely in-memory -- an unexported map plus a `scheduled`
-- counter that reset to zero on every foundryd restart, so the active-mission
-- cap, the per-mission budget-isolation ceiling and the fairness bound were all
-- fail-OPEN across a restart. Persisting activation, spend-to-date, the schedule
-- counter and last-scheduled-at makes every one of those invariants survive a
-- `kill -9`, and the portfolio_state.version column serializes activation so two
-- workers can never both activate past the cap.

CREATE TABLE IF NOT EXISTS portfolio_state (
    portfolio_id        TEXT PRIMARY KEY,
    max_active_products INTEGER NOT NULL DEFAULT 0,
    version             BIGINT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT portfolio_state_max_active_nonneg CHECK (max_active_products >= 0)
);

COMMENT ON TABLE portfolio_state IS 'Authoritative (docs/PLAN.md Task 121, Constitution C18/C19): one row per portfolio. version is bumped under a FOR UPDATE row lock on every activation/deactivation/schedule so the active-mission cap cannot be raced past by two concurrent workers.';

CREATE TABLE IF NOT EXISTS portfolio_schedule (
    portfolio_id       TEXT NOT NULL REFERENCES portfolio_state (portfolio_id) ON DELETE CASCADE,
    mission_id         TEXT NOT NULL,
    active             BOOLEAN NOT NULL DEFAULT false,
    revenue_bearing    BOOLEAN NOT NULL DEFAULT false,
    monthly_budget_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    spent_usd          DOUBLE PRECISION NOT NULL DEFAULT 0,
    scheduled          BIGINT NOT NULL DEFAULT 0,
    last_scheduled_at  TIMESTAMPTZ,
    -- budget_scope is the cost-ledger scope_id (Task 29 ScopeMission) this
    -- mission's envelope is attributed to, so budget isolation is provable
    -- against cost_entries/budgets rather than only against this table.
    budget_scope       TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (portfolio_id, mission_id),
    CONSTRAINT portfolio_schedule_budget_nonneg CHECK (monthly_budget_usd >= 0 AND spent_usd >= 0),
    CONSTRAINT portfolio_schedule_scheduled_nonneg CHECK (scheduled >= 0)
);

COMMENT ON TABLE portfolio_schedule IS 'Authoritative (docs/PLAN.md Task 121): per-mission activation, spend-to-date, fair-schedule counter and last-scheduled-at. Every mutable portfolio field lives here so ActiveCount/SpentUSD/scheduled survive a restart instead of resetting to zero.';

CREATE INDEX IF NOT EXISTS portfolio_schedule_active_idx
    ON portfolio_schedule (portfolio_id, active);

-- +goose Down
DROP TABLE IF EXISTS portfolio_schedule;
DROP TABLE IF EXISTS portfolio_state;
