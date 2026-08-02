-- +goose Up
-- docs/PLAN.md Task 145 (INT-08): durable Telegram drafts, bindings, nonces, audit.
CREATE TABLE IF NOT EXISTS telegram_chat_bindings (
    chat_id      TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL,
    profile_id   TEXT NOT NULL DEFAULT '',
    bot_id       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS telegram_drafts (
    draft_id        TEXT PRIMARY KEY,
    chat_id         TEXT NOT NULL,
    principal_id    TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('IDEA', 'MOCKUP')),
    content_hash    TEXT NOT NULL,
    content_text    TEXT NOT NULL DEFAULT '',
    artifact_ref    TEXT NOT NULL DEFAULT '',
    artifact_digest TEXT NOT NULL DEFAULT '',
    budget_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    nonce_hash      TEXT NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ,
    confirmed_run   TEXT NOT NULL DEFAULT '',
    confirmed_mission TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS telegram_drafts_nonce_uidx
    ON telegram_drafts (nonce_hash)
    WHERE nonce_hash <> '' AND used_at IS NULL;

CREATE TABLE IF NOT EXISTS telegram_command_audit (
    id           BIGSERIAL PRIMARY KEY,
    chat_id      TEXT NOT NULL,
    principal_id TEXT NOT NULL DEFAULT '',
    command      TEXT NOT NULL,
    update_id    BIGINT NOT NULL DEFAULT 0,
    result       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS telegram_command_audit_update_uidx
    ON telegram_command_audit (chat_id, update_id)
    WHERE update_id <> 0;

-- +goose Down
DROP INDEX IF EXISTS telegram_command_audit_update_uidx;
DROP TABLE IF EXISTS telegram_command_audit;
DROP INDEX IF EXISTS telegram_drafts_nonce_uidx;
DROP TABLE IF EXISTS telegram_drafts;
DROP TABLE IF EXISTS telegram_chat_bindings;
