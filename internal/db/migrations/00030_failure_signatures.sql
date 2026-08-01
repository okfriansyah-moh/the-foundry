-- +goose Up
-- Task 123 (MMR-03): per-attempt task failure signatures. Task 94's liveness
-- supervisor has five conditions; two of them -- PoisonedTask and InfiniteRetry
-- -- classify against WorkflowSnapshot.RecentFailures, which no code path in
-- this repo populated (PostgresProjectionSource left it nil by design). This
-- table is the durable failure-signature history the kernel's runTask writes on
-- every failed attempt, so those two conditions fire against live Postgres data
-- rather than remaining undetectable. The digest is a NORMALIZED fingerprint
-- (classification + stable task detail, no timestamps or paths), so "the same
-- failure N times" is a stable comparison rather than a string match.
--
-- UNIQUE (workflow_id, task_id, attempt) makes recording idempotent: a Temporal
-- retry of the record activity, or a crash between the write and the receipt,
-- addresses the same row via ON CONFLICT DO NOTHING instead of inflating the
-- history with duplicates (Constitution C9).

CREATE TABLE IF NOT EXISTS task_failure_signatures (
    id             TEXT PRIMARY KEY,
    workflow_id    TEXT NOT NULL,
    task_id        TEXT NOT NULL,
    attempt        INTEGER NOT NULL,
    classification TEXT NOT NULL,
    detail_digest  TEXT NOT NULL,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, task_id, attempt)
);

COMMENT ON TABLE task_failure_signatures IS 'Authoritative (docs/PLAN.md Task 123, Constitution C22): per-attempt task failure signatures the liveness supervisor''s PoisonedTask/InfiniteRetry conditions classify against. detail_digest is a normalized fingerprint so identical failures compare equal.';

CREATE INDEX IF NOT EXISTS task_failure_signatures_wf_idx
    ON task_failure_signatures (workflow_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS task_failure_signatures_wf_task_idx
    ON task_failure_signatures (workflow_id, task_id, occurred_at ASC);

-- +goose Down
DROP TABLE IF EXISTS task_failure_signatures;
