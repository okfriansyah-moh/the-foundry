-- +goose Up
-- docs/PLAN.md Task 144 (INT-07): durable production intake stage references.
CREATE TABLE IF NOT EXISTS intake_runtime (
    run_id              TEXT PRIMARY KEY,
    opportunity_id      TEXT NOT NULL DEFAULT '',
    verdict             TEXT NOT NULL DEFAULT '',
    approval_ref        TEXT NOT NULL DEFAULT '',
    mission_id          TEXT NOT NULL DEFAULT '',
    workflow_id         TEXT NOT NULL DEFAULT '',
    envelope_digest     TEXT NOT NULL DEFAULT '',
    cost_usd            DOUBLE PRECISION NOT NULL DEFAULT 0,
    idempotency_key     TEXT NOT NULL DEFAULT '',
    last_stage          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS intake_runtime_idem_uidx
    ON intake_runtime (idempotency_key)
    WHERE idempotency_key <> '';

-- +goose Down
DROP INDEX IF EXISTS intake_runtime_idem_uidx;
DROP TABLE IF EXISTS intake_runtime;
