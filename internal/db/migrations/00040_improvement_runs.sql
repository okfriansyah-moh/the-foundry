-- +goose Up
-- docs/PLAN.md Task 147 (VEN-19): durable improvement loop run records.
CREATE TABLE IF NOT EXISTS improvement_runs (
    run_id              TEXT PRIMARY KEY,
    mission_id          TEXT NOT NULL,
    product_id          TEXT NOT NULL,
    lease_id            TEXT NOT NULL DEFAULT '',
    plan_id             TEXT NOT NULL DEFAULT '',
    plan_digest         TEXT NOT NULL DEFAULT '',
    envelope_digest     TEXT NOT NULL DEFAULT '',
    delivery_workflow   TEXT NOT NULL DEFAULT '',
    deploy_receipt      TEXT NOT NULL DEFAULT '',
    promotion_id        TEXT NOT NULL DEFAULT '',
    observation_ref     TEXT NOT NULL DEFAULT '',
    rollback_ref        TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'running',
    result_code         TEXT NOT NULL DEFAULT '',
    idempotency_key     TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS improvement_runs_idem_uidx
    ON improvement_runs (idempotency_key)
    WHERE idempotency_key <> '';

CREATE UNIQUE INDEX IF NOT EXISTS improvement_runs_active_product_uidx
    ON improvement_runs (product_id)
    WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS improvement_runs_active_product_uidx;
DROP INDEX IF EXISTS improvement_runs_idem_uidx;
DROP TABLE IF EXISTS improvement_runs;
