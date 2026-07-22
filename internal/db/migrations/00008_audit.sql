-- +goose Up
-- docs/PLAN.md Task 20 (FND-01): audit log hash chain. seq/prev_hash/hash
-- form a tamper-evident chain, but the chaining is computed by a
-- trigger-free Go writer (internal/db or a later audit package) — this
-- migration creates table shape only, no DB trigger.

CREATE TABLE IF NOT EXISTS audit_log (
    seq        BIGSERIAL PRIMARY KEY,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    subject    TEXT NOT NULL,
    payload    JSONB NOT NULL,
    prev_hash  BYTEA,
    hash       BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE audit_log IS 'Authoritative (Constitution C3, data-consistency.md §1): append-only audit hash chain. Chain computed by a trigger-free Go writer, not a DB trigger — this table only defines shape.';

-- +goose Down
DROP TABLE IF EXISTS audit_log;
