-- +goose Up
-- docs/PLAN.md Task 143 (RTC-06): owned repository registry.
CREATE TABLE IF NOT EXISTS repository_registry (
    id                     TEXT PRIMARY KEY,
    provider               TEXT NOT NULL,
    canonical_url          TEXT NOT NULL,
    alias                  TEXT NOT NULL DEFAULT '',
    profile_id             TEXT NOT NULL,
    organization_id        TEXT NOT NULL DEFAULT '',
    pinned_base_revision   TEXT NOT NULL DEFAULT '',
    default_target_branch  TEXT NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT repository_registry_provider_chk
      CHECK (provider IN ('github', 'bitbucket', 'local'))
);

CREATE UNIQUE INDEX IF NOT EXISTS repository_registry_url_uidx
    ON repository_registry (canonical_url);

CREATE INDEX IF NOT EXISTS repository_registry_profile_idx
    ON repository_registry (profile_id);

-- +goose Down
DROP INDEX IF EXISTS repository_registry_profile_idx;
DROP INDEX IF EXISTS repository_registry_url_uidx;
DROP TABLE IF EXISTS repository_registry;
