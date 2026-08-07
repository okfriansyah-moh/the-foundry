-- +goose Up
-- docs/PLAN.md Tasks 156-161 (CFG-01..05, CAP-04):
-- operator-hot configuration source of truth in Postgres.
CREATE TABLE IF NOT EXISTS operator_config_entries (
    config_key      TEXT PRIMARY KEY,
    active_version  BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS operator_config_versions (
    config_key      TEXT NOT NULL REFERENCES operator_config_entries (config_key) ON DELETE CASCADE,
    version         BIGINT NOT NULL CHECK (version >= 1),
    payload         BYTEA NOT NULL,
    payload_sha256  TEXT NOT NULL CHECK (payload_sha256 <> ''),
    proposal_ref    TEXT NOT NULL DEFAULT '',
    approved_by     TEXT NOT NULL DEFAULT '',
    reviewer        TEXT NOT NULL DEFAULT '',
    implementer     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (config_key, version),
    CHECK (reviewer = '' OR implementer = '' OR reviewer <> implementer)
);

CREATE TABLE IF NOT EXISTS operator_config_apply_audit (
    id              BIGSERIAL PRIMARY KEY,
    config_key      TEXT NOT NULL,
    version         BIGINT NOT NULL,
    proposal_ref    TEXT NOT NULL,
    approved_by     TEXT NOT NULL,
    reviewer        TEXT NOT NULL,
    implementer     TEXT NOT NULL,
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (config_key, version),
    FOREIGN KEY (config_key, version)
        REFERENCES operator_config_versions (config_key, version)
        ON DELETE CASCADE,
    CHECK (reviewer <> implementer)
);

CREATE INDEX IF NOT EXISTS operator_config_versions_created_idx
    ON operator_config_versions (config_key, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS operator_config_versions_created_idx;
DROP TABLE IF EXISTS operator_config_apply_audit;
DROP TABLE IF EXISTS operator_config_versions;
DROP TABLE IF EXISTS operator_config_entries;
