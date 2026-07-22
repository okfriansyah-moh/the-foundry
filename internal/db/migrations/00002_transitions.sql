-- +goose Up
-- docs/PLAN.md Task 12 (SKP-10): three tables the kernel workflow needs to
-- do its job durably outside of Temporal's own history — the transition
-- stream (source of Task 14's projection), lease/fencing tokens for
-- mutating worktree ops, and idempotency receipts so a re-executed
-- activity returns its recorded outcome instead of re-running a side
-- effect. Ported byte-for-semantically-identical from
-- migrations/0002_transitions.sql into goose format by Task 20 (FND-01) —
-- schema itself is unchanged.

CREATE TABLE IF NOT EXISTS workflow_transitions (
    workflow_id TEXT NOT NULL,
    seq         BIGSERIAL,
    payload     JSONB NOT NULL,
    PRIMARY KEY (workflow_id, seq)
);

CREATE INDEX IF NOT EXISTS workflow_transitions_workflow_id_idx
    ON workflow_transitions (workflow_id, seq);

CREATE TABLE IF NOT EXISTS leases (
    resource   TEXT PRIMARY KEY,
    token      TEXT NOT NULL,
    holder     TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS receipts (
    key        TEXT PRIMARY KEY,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE workflow_transitions IS 'Authoritative (Constitution C3/C4): kernel-written transition stream, source of truth feeding the Task 14 status projection. Not itself a projection.';
COMMENT ON TABLE leases IS 'Authoritative (Constitution C4): lease/fencing tokens for mutating worktree operations.';
COMMENT ON TABLE receipts IS 'Authoritative (Constitution C4): idempotency receipts for kernel-executed side effects.';

-- +goose Down
DROP TABLE IF EXISTS receipts;
DROP TABLE IF EXISTS leases;
DROP TABLE IF EXISTS workflow_transitions;
