-- +goose Up
-- docs/PLAN.md Task 148 (TX-13): TenX orchestration durable refs.
CREATE TABLE IF NOT EXISTS tenx_orchestration_runs (
    run_id              TEXT PRIMARY KEY,
    approved_plan_id    TEXT NOT NULL,
    plan_digest         TEXT NOT NULL DEFAULT '',
    envelope_digest     TEXT NOT NULL DEFAULT '',
    organization_id     TEXT NOT NULL DEFAULT '',
    profile_id          TEXT NOT NULL DEFAULT '',
    wave_digest         TEXT NOT NULL DEFAULT '',
    atomic_group_id     TEXT NOT NULL DEFAULT '',
    manifest_digest     TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'running',
    result_code         TEXT NOT NULL DEFAULT '',
    idempotency_key     TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS tenx_orchestration_idem_uidx
    ON tenx_orchestration_runs (idempotency_key)
    WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS tenx_orchestration_receipts (
    receipt_id          TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL REFERENCES tenx_orchestration_runs(run_id),
    repository_id       TEXT NOT NULL DEFAULT '',
    provider            TEXT NOT NULL DEFAULT '',
    branch              TEXT NOT NULL DEFAULT '',
    before_sha          TEXT NOT NULL DEFAULT '',
    after_sha           TEXT NOT NULL DEFAULT '',
    atomic_group_id     TEXT NOT NULL DEFAULT '',
    manifest_digest     TEXT NOT NULL DEFAULT '',
    envelope_digest     TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS tenx_orchestration_receipts;
DROP INDEX IF EXISTS tenx_orchestration_idem_uidx;
DROP TABLE IF EXISTS tenx_orchestration_runs;
