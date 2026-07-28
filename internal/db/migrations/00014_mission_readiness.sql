-- +goose Up
-- docs/PLAN.md Task 41 (VEN-02): Mission Setup Ceremony readiness artifact.
-- Persist signed ceremony output so unattended runtime can be hard-gated on
-- readiness pass.

CREATE TABLE IF NOT EXISTS mission_readiness_artifacts (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions (id),
    readiness   TEXT NOT NULL CHECK (readiness IN ('pass', 'fail')),
    approved_by TEXT NOT NULL,
    digest      TEXT NOT NULL,
    artifact    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mission_readiness_artifacts_mission_id_idx
    ON mission_readiness_artifacts (mission_id, created_at DESC);

COMMENT ON TABLE mission_readiness_artifacts IS 'Authoritative mission ceremony readiness artifacts (Task 41 / C17).';

-- +goose Down
DROP TABLE IF EXISTS mission_readiness_artifacts;
