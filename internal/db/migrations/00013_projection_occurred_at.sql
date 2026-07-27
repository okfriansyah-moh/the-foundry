-- +goose Up
-- Correctness fix, found live by Task 39 (FND-20, M1 exit drill) and
-- implemented here per its own recommendation (docs/notes/m1-exit-report.md
-- "Finding: internal/projection's out-of-order idempotency guard does not
-- hold"): upsertProjectionSQL's ON CONFLICT guard (00003_projection.sql)
-- compared only on `last_seq`, a pure sequence-monotonicity check. That
-- protects against reprocessing an exact-duplicate seq, but does NOT
-- protect against a stale/superseded transition redelivered at a NEW,
-- higher seq carrying OLDER semantic content (e.g. a delayed backfill/
-- replay inserting a historical transition out of band) — such a row would
-- regress the projected phase backward, contradicting Task 14's own
-- Acceptance ("out-of-order/duplicate seq handled idempotently") and
-- upsertProjectionSQL's own doc comment.
--
-- Fix: guard on the semantically-ordered (occurred_at, last_seq) tuple
-- instead of last_seq alone (internal/projection/projector.go). This column
-- persists state.Transition.OccurredAt (already present on every transition
-- payload, decoded by decodeTransition) so the guard has that ordering key
-- to compare against on every future write, including reads back after a
-- crash/restart.
ALTER TABLE workflow_status_projection ADD COLUMN IF NOT EXISTS occurred_at TIMESTAMPTZ;
ALTER TABLE workflow_status_projection_shadow ADD COLUMN IF NOT EXISTS occurred_at TIMESTAMPTZ;

-- +goose StatementBegin
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
                COALESCE(occurred_at::text, '') || '|' ||
                projector_version
            ) AS row_digest
        FROM workflow_status_projection
    ) rows;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION projection_checksum_shadow() RETURNS TEXT AS $$
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
                COALESCE(occurred_at::text, '') || '|' ||
                projector_version
            ) AS row_digest
        FROM workflow_status_projection_shadow
    ) rows;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION projection_checksum_shadow() RETURNS TEXT AS $$
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
        FROM workflow_status_projection_shadow
    ) rows;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

ALTER TABLE workflow_status_projection_shadow DROP COLUMN IF EXISTS occurred_at;
ALTER TABLE workflow_status_projection DROP COLUMN IF EXISTS occurred_at;
