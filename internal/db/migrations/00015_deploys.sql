-- +goose Up
-- docs/PLAN.md Task 47 (VEN-08): deploy records + verification mode + gate results.

CREATE TABLE IF NOT EXISTS deploy_records (
    id                TEXT PRIMARY KEY,
    product           TEXT NOT NULL,
    environment       TEXT NOT NULL,
    ref               TEXT NOT NULL,
    verification_mode TEXT NOT NULL,
    gate_results      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deploy_records_product_idx
    ON deploy_records (product, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS deploy_records;
