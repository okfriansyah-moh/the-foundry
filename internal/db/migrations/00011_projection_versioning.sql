-- +goose Up
-- docs/PLAN.md Task 38 (FND-19): versioned-projector rollout tooling
-- (internal/projection/versioning.go). data-consistency.md §2: "projector
-- schema migrations: deploy new projector version alongside, backfill,
-- cut over, then retire — never in-place mutation of live projection
-- semantics." This shadow table is the "alongside" deployment target;
-- versioning.go's Rollout performs the backfill, checksum-compare, and
-- atomic rename cut-over.

CREATE TABLE IF NOT EXISTS workflow_status_projection_shadow (
    LIKE workflow_status_projection INCLUDING ALL
);

COMMENT ON TABLE workflow_status_projection_shadow IS 'Shadow projection table for versioned-projector rollout (docs/PLAN.md Task 38; Constitution C3 rebuildable projection). Never read by any API/CLI status path -- internal/projection/versioning.go Rollout is the only writer, until the atomic swap promotes it to workflow_status_projection.';

-- projection_checksum_shadow() mirrors projection_checksum()
-- (00003_projection.sql) exactly, but over the shadow table. Rollout uses
-- it to prove the new projector version's backfill is reproducible --
-- truncate-and-replay the shadow table twice; the digests must match if
-- nothing new arrived between the two passes -- the same
-- "drop table -> rebuild -> identical checksum" contract Task 14's
-- Rebuild() already established, applied here to the shadow table before
-- it is ever cut over to live.
--
-- StatementBegin/StatementEnd: without this fencing goose's naive ';'
-- splitter cuts the dollar-quoted body mid-string (see the fix applied to
-- 00003_projection.sql's projection_checksum() in this same task).
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

-- +goose Down
DROP FUNCTION IF EXISTS projection_checksum_shadow();
DROP TABLE IF EXISTS workflow_status_projection_shadow;
