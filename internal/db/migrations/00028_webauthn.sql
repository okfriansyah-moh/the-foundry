-- +goose Up
-- Task 114 (INT-06): durable WebAuthn strong-auth state. Credentials, challenge
-- sessions and signature counters previously lived in an in-memory store and
-- died on every foundryd restart; persisting them makes strong-auth survive a
-- restart and preserves clone detection (sign-count regression) across it.

CREATE TABLE IF NOT EXISTS webauthn_credentials (
    principal     TEXT NOT NULL,
    credential_id TEXT NOT NULL,
    aaguid        TEXT NOT NULL DEFAULT '',
    sign_count    BIGINT NOT NULL DEFAULT 0,
    credential    JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal, credential_id)
);

CREATE INDEX IF NOT EXISTS webauthn_credentials_principal_idx
    ON webauthn_credentials (principal);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
    id         TEXT PRIMARY KEY,
    data       JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS webauthn_sessions_expiry_idx
    ON webauthn_sessions (expires_at ASC);

-- +goose Down
DROP TABLE IF EXISTS webauthn_sessions;
DROP TABLE IF EXISTS webauthn_credentials;
