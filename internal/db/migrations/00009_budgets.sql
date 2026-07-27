-- +goose Up
-- docs/PLAN.md Task 29 (FND-10): cost ledger v1 — budget envelopes
-- (Constitution C19, docs/foundry/docs/operations/cost-accounting.md
-- §2/§3). cost_entries (Task 20/00006_ledgers.sql) already carries the
-- reserved/estimated/incurred/reconciled state machine for individual
-- ledger entries; this migration adds the aggregate ceiling
-- internal/ledger/cost.Store.Reserve checks atomically against, plus two
-- states cost-accounting.md requires that Task 20 did not anticipate:
-- 'released' (an unspent reservation returned to its envelope) and
-- 'shadow' (subscription-priced entries with no real reservation, §1).

CREATE TABLE IF NOT EXISTS budgets (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL CHECK (scope IN ('workflow', 'product', 'mission')),
    scope_id     TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('mission_monthly', 'provider', 'infra', 'experiment', 'reserve')),
    period       TEXT NOT NULL,
    ceiling_usd  NUMERIC(12, 4) NOT NULL,
    reserved_usd NUMERIC(12, 4) NOT NULL DEFAULT 0,
    incurred_usd NUMERIC(12, 4) NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, scope_id, kind, period)
);

CREATE INDEX IF NOT EXISTS budgets_scope_idx ON budgets (scope, scope_id);

COMMENT ON TABLE budgets IS 'Authoritative (Constitution C19, cost-accounting.md §2/§3): budget envelopes. ceiling_usd - (reserved_usd + incurred_usd) is the atomically-checked available amount Store.Reserve enforces via a single UPDATE...WHERE (row-lock serialized, not a read-then-write race).';

ALTER TABLE cost_entries DROP CONSTRAINT IF EXISTS cost_entries_state_check;
ALTER TABLE cost_entries
    ADD CONSTRAINT cost_entries_state_check
    CHECK (state IN ('reserved', 'estimated', 'incurred', 'reconciled', 'released', 'shadow'));

ALTER TABLE cost_entries ADD COLUMN IF NOT EXISTS budget_id TEXT REFERENCES budgets (id);

-- +goose Down
ALTER TABLE cost_entries DROP COLUMN IF EXISTS budget_id;
ALTER TABLE cost_entries DROP CONSTRAINT IF EXISTS cost_entries_state_check;
ALTER TABLE cost_entries
    ADD CONSTRAINT cost_entries_state_check
    CHECK (state IN ('reserved', 'estimated', 'incurred', 'reconciled'));
DROP TABLE IF EXISTS budgets;
