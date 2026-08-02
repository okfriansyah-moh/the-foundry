-- +goose Up
-- docs/PLAN.md Task 141 (RTC-05): immutable kernel-resolved execution envelopes.
-- Append-only: no UPDATE path. Revocation is recorded as a separate column
-- write that never mutates authority fields or the digest.
CREATE TABLE IF NOT EXISTS execution_envelopes (
    envelope_id              TEXT PRIMARY KEY,
    envelope_digest          TEXT NOT NULL UNIQUE,
    schema_version           TEXT NOT NULL,
    payload                  JSONB NOT NULL,
    approved_plan_id         TEXT NOT NULL,
    plan_digest              TEXT NOT NULL,
    mission_id               TEXT NOT NULL DEFAULT '',
    portfolio_id             TEXT NOT NULL DEFAULT '',
    profile_id               TEXT NOT NULL DEFAULT '',
    organization_id          TEXT NOT NULL DEFAULT '',
    principal_id             TEXT NOT NULL DEFAULT '',
    policy_digest            TEXT NOT NULL,
    approval_ref             TEXT NOT NULL DEFAULT '',
    issued_at                TIMESTAMPTZ NOT NULL,
    expires_at               TIMESTAMPTZ,
    revoked                  BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at               TIMESTAMPTZ,
    revocation_reason        TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS execution_envelopes_plan_idx
    ON execution_envelopes (approved_plan_id, created_at ASC);

CREATE INDEX IF NOT EXISTS execution_envelopes_mission_idx
    ON execution_envelopes (mission_id)
    WHERE mission_id <> '';

CREATE INDEX IF NOT EXISTS execution_envelopes_digest_idx
    ON execution_envelopes (envelope_digest);

-- +goose Down
DROP INDEX IF EXISTS execution_envelopes_digest_idx;
DROP INDEX IF EXISTS execution_envelopes_mission_idx;
DROP INDEX IF EXISTS execution_envelopes_plan_idx;
DROP TABLE IF EXISTS execution_envelopes;
