-- +goose Up
-- docs/PLAN.md Task 55 (TX-02): org plan provenance source records.
-- Stores the source repo/revision/digest set declared in org plans so
-- validators can re-verify provenance after admission.

CREATE TABLE IF NOT EXISTS org_provenance_sources (
    id             TEXT PRIMARY KEY,
    plan_id        TEXT NOT NULL,
    repo           TEXT NOT NULL,
    revision       TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS org_provenance_sources_plan_idx
    ON org_provenance_sources (plan_id);

CREATE TABLE IF NOT EXISTS org_provenance_digests (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL REFERENCES org_provenance_sources (id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    digest      TEXT NOT NULL,     -- SHA-256 hex
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS org_provenance_digests_source_idx
    ON org_provenance_digests (source_id);

-- +goose Down
DROP TABLE IF EXISTS org_provenance_digests;
DROP TABLE IF EXISTS org_provenance_sources;
