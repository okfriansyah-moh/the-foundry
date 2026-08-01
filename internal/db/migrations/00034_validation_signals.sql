-- +goose Up
-- docs/PLAN.md Task 139 (OPP-05): provenance-backed validation signals.
CREATE TABLE IF NOT EXISTS validation_signals (
    id                   TEXT PRIMARY KEY,
    opportunity_id       TEXT NOT NULL,
    class                TEXT NOT NULL,
    source_identity      TEXT NOT NULL,
    source_ref           TEXT NOT NULL,
    experiment_id        TEXT NOT NULL,
    hypothesis           TEXT NOT NULL,
    sample_size          INTEGER NOT NULL,
    sample_denominator   INTEGER NOT NULL,
    observed_at          TIMESTAMPTZ NOT NULL,
    acquisition_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    currency             TEXT NOT NULL DEFAULT '',
    environment          TEXT NOT NULL,
    payload_digest       TEXT NOT NULL,
    raw_payload          BYTEA NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    idempotency_key      TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS validation_signals_idem_uidx
    ON validation_signals (idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX IF NOT EXISTS validation_signals_opp_idx
    ON validation_signals (opportunity_id, created_at ASC);

-- +goose Down
DROP TABLE IF EXISTS validation_signals;
