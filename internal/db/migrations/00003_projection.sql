-- +goose Up
-- docs/PLAN.md Task 14 (SKP-12): rebuildable PostgreSQL read model of
-- workflow status fed by Task 12's workflow_transitions stream
-- (Constitution C3 — a projection, never execution authority;
-- docs/foundry/docs/architecture/data-consistency.md §2 projection
-- contract). Ported byte-for-semantically-identical from
-- migrations/0003_projection.sql into goose format by Task 20 (FND-01) —
-- schema itself is unchanged.

CREATE TABLE IF NOT EXISTS workflow_status_projection (
    workflow_id       TEXT PRIMARY KEY,
    status            TEXT,
    phase             TEXT,
    reason            TEXT,
    result_code       TEXT,
    attempt           INT,
    checkpoint_id     TEXT,
    wake_at           TIMESTAMPTZ,
    last_seq          BIGINT NOT NULL,
    projector_version TEXT NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS projection_offsets (
    projector TEXT PRIMARY KEY,
    last_seq  BIGINT NOT NULL
);

-- projection_checksum() digests the whole projection table deterministically
-- (rows ordered by workflow_id, so insertion order never affects the
-- result) so `foundry projection rebuild` and its e2e test can prove a
-- truncate-and-replay reproduces byte-identical projected state
-- (data-consistency.md §2: "rebuild is a routine, tested operation").
CREATE OR REPLACE FUNCTION projection_checksum() RETURNS TEXT AS $$
    SELECT COALESCE(
        md5(string_agg(row_digest, '|' ORDER BY workflow_id)),
        md5('')
    )
    FROM (
        SELECT
            workflow_id,
            md5(
                workflow_id || '|' ||
                COALESCE(status, '') || '|' ||
                COALESCE(phase, '') || '|' ||
                COALESCE(reason, '') || '|' ||
                COALESCE(result_code, '') || '|' ||
                COALESCE(attempt::text, '') || '|' ||
                COALESCE(checkpoint_id, '') || '|' ||
                COALESCE(wake_at::text, '') || '|' ||
                last_seq::text || '|' ||
                projector_version
            ) AS row_digest
        FROM workflow_status_projection
    ) rows;
$$ LANGUAGE sql STABLE;

COMMENT ON TABLE workflow_status_projection IS 'Rebuildable projection (Constitution C3): NEVER execution authority. Fed idempotently from workflow_transitions by a versioned projector; truncate-and-replay must reproduce byte-identical state.';
COMMENT ON TABLE projection_offsets IS 'Rebuildable projection bookkeeping (Constitution C3): per-projector last-applied sequence offset, not authoritative on its own.';

-- +goose Down
DROP FUNCTION IF EXISTS projection_checksum();
DROP TABLE IF EXISTS projection_offsets;
DROP TABLE IF EXISTS workflow_status_projection;
