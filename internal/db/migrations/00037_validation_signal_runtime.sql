-- +goose Up
-- docs/PLAN.md Task 146 (OPP-06): validation-signal runtime lifecycle fields.
ALTER TABLE validation_signals
    ADD COLUMN IF NOT EXISTS profile_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS revoked BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS revocation_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS acquisition_reservation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS raw_artifact_ref TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS validation_signals_profile_idx
    ON validation_signals (profile_id)
    WHERE profile_id <> '';

-- +goose Down
DROP INDEX IF EXISTS validation_signals_profile_idx;
ALTER TABLE validation_signals
    DROP COLUMN IF EXISTS raw_artifact_ref,
    DROP COLUMN IF EXISTS acquisition_reservation_id,
    DROP COLUMN IF EXISTS revocation_reason,
    DROP COLUMN IF EXISTS revoked_at,
    DROP COLUMN IF EXISTS revoked,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS profile_id;
