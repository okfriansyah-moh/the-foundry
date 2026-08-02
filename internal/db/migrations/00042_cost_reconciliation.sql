-- +goose Up
-- docs/PLAN.md Task 149 (SEC-06): cost reconciliation backlog + freeze linkage.
CREATE TABLE IF NOT EXISTS cost_reconciliation_backlog (
    entry_id            TEXT PRIMARY KEY,
    profile_id          TEXT NOT NULL,
    mission_id          TEXT NOT NULL DEFAULT '',
    provider_usage_ref  TEXT NOT NULL DEFAULT '',
    kind                TEXT NOT NULL CHECK (kind IN ('observed','derived','shadow','unreconciled')),
    amount_usd          DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_text          TEXT NOT NULL DEFAULT '',
    attempts            INT NOT NULL DEFAULT 0,
    freeze_triggered    BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cost_reconciliation_backlog_profile_idx
    ON cost_reconciliation_backlog (profile_id, kind);

CREATE TABLE IF NOT EXISTS profile_runtime_isolation (
    profile_id          TEXT PRIMARY KEY,
    db_identity         TEXT NOT NULL,
    temporal_namespace  TEXT NOT NULL,
    evidence_prefix     TEXT NOT NULL,
    secret_scope        TEXT NOT NULL,
    scm_identity        TEXT NOT NULL DEFAULT '',
    deploy_identity     TEXT NOT NULL DEFAULT '',
    billing_identity    TEXT NOT NULL DEFAULT '',
    telegram_scope      TEXT NOT NULL DEFAULT '',
    validated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS profile_runtime_isolation;
DROP INDEX IF EXISTS cost_reconciliation_backlog_profile_idx;
DROP TABLE IF EXISTS cost_reconciliation_backlog;
