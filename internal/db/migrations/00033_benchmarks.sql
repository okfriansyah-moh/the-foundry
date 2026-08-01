-- +goose Up
-- Task 134 (ACC-01): durable V1 acceleration benchmark run records (Constitution
-- C25). Each row stores a full RunRecord JSON payload so control and foundry
-- arms can be compared with explicit measurement bases.

CREATE TABLE IF NOT EXISTS benchmark_runs (
    id                 TEXT PRIMARY KEY,
    arm                TEXT NOT NULL CHECK (arm IN ('control', 'foundry')),
    work_item_id       TEXT NOT NULL,
    recorded_at        TIMESTAMPTZ NOT NULL,
    environment_digest TEXT NOT NULL,
    payload            JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS benchmark_runs_arm_recorded_at_idx
    ON benchmark_runs (arm, recorded_at);

COMMENT ON TABLE benchmark_runs IS 'Authoritative (docs/PLAN.md Task 134, Constitution C25): V1 acceleration RunRecords per arm. Payload is the canonical RunRecord JSON; arm-tagged comparisons require matching environment digests.';

-- +goose Down
DROP TABLE IF EXISTS benchmark_runs;
