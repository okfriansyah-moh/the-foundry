-- +goose Up
-- docs/PLAN.md Task 58 (TX-05): integration queue + receipts.
-- The Branch Integrator serializes per-branch pushes; advisory lock
-- is taken on hash(branch) at runtime. This migration creates the
-- persistent backing store for the queue and receipts.

CREATE TABLE IF NOT EXISTS integration_queue (
    id               TEXT PRIMARY KEY,
    branch           TEXT NOT NULL,
    group_id         TEXT NOT NULL,
    manifest_digest  TEXT NOT NULL,
    commits          TEXT[] NOT NULL DEFAULT '{}',
    expected_base    TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',  -- pending|processing|done|failed|requeued
    enqueued_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ,
    error_msg        TEXT
);

CREATE INDEX IF NOT EXISTS integration_queue_branch_idx
    ON integration_queue (branch, enqueued_at ASC)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS integration_receipts (
    id               TEXT PRIMARY KEY,
    branch           TEXT NOT NULL,
    before_sha       TEXT NOT NULL,
    after_sha        TEXT NOT NULL,
    group_id         TEXT NOT NULL,
    manifest_digest  TEXT NOT NULL,
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS integration_receipts_branch_idx
    ON integration_receipts (branch, issued_at DESC);

-- +goose Down
DROP TABLE IF EXISTS integration_receipts;
DROP TABLE IF EXISTS integration_queue;
