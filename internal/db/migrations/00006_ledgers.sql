-- +goose Up
-- docs/PLAN.md Task 20 (FND-01): external-operation ledger (outbox for
-- kernel-only side effects, Constitution C4/C9 — Task 26/27 own the
-- business logic) and cost ledger (Task 29 owns the business logic).
-- This migration creates shape only.

CREATE TABLE IF NOT EXISTS external_operations (
    id              TEXT PRIMARY KEY,
    workflow_id     TEXT NOT NULL,
    kind            TEXT NOT NULL,
    target          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    state           TEXT NOT NULL CHECK (state IN ('reserved', 'executed', 'reconciled', 'failed')),
    request         JSONB NOT NULL,
    receipt         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS external_operations_workflow_id_idx ON external_operations (workflow_id);

CREATE TABLE IF NOT EXISTS cost_entries (
    id             TEXT PRIMARY KEY,
    scope          TEXT NOT NULL CHECK (scope IN ('workflow', 'product', 'mission')),
    scope_id       TEXT NOT NULL,
    state          TEXT NOT NULL CHECK (state IN ('reserved', 'estimated', 'incurred', 'reconciled')),
    amount_usd     NUMERIC(12, 4) NOT NULL,
    pricing_version TEXT NOT NULL,
    provider       TEXT NOT NULL,
    meta           JSONB,
    at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cost_entries_scope_idx ON cost_entries (scope, scope_id);

COMMENT ON TABLE external_operations IS 'Authoritative (Constitution C4/C9, data-consistency.md §1): kernel-owned external-operation ledger/outbox — the only path for cross-store side effects.';
COMMENT ON TABLE cost_entries IS 'Authoritative (Constitution C3/C19, data-consistency.md §1): cost ledger entries.';

-- +goose Down
DROP TABLE IF EXISTS cost_entries;
DROP TABLE IF EXISTS external_operations;
