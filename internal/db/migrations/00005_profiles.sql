-- +goose Up
-- docs/PLAN.md Task 20 (FND-01): profile records used by policy
-- compilation (Task 22) and approvals. Owned by internal/profile
-- (Task 21); this migration creates shape only.

CREATE TABLE IF NOT EXISTS profiles (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('personal', 'organization')),
    org_id         TEXT REFERENCES organizations (id),
    config         JSONB NOT NULL,
    policy_digest  TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE profiles IS 'Authoritative (Constitution C3, data-consistency.md §1): profile config + resolved policy digest.';

-- +goose Down
DROP TABLE IF EXISTS profiles;
