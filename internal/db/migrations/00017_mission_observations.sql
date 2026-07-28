-- +goose Up
-- docs/PLAN.md Task 50 (VEN-11): mission observations + decide records.

CREATE TABLE IF NOT EXISTS mission_observations (
    id                 TEXT PRIMARY KEY,
    mission_id         TEXT NOT NULL REFERENCES missions (id),
    observed_at        TIMESTAMPTZ NOT NULL,
    activation_rate    NUMERIC(8,4) NOT NULL,
    conversion_rate    NUMERIC(8,4) NOT NULL,
    net_mrr_usd        NUMERIC(12,4) NOT NULL,
    cost_to_date_usd   NUMERIC(12,4) NOT NULL,
    decide             TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mission_observations_mission_idx
    ON mission_observations (mission_id, observed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS mission_observations;
