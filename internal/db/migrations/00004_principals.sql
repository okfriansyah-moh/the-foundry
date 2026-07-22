-- +goose Up
-- docs/PLAN.md Task 20 (FND-01): identity substrate for M1 — principals
-- (human or service actors), organizations, and org membership. Owned by
-- internal/identity (Task 21); this migration creates shape only, no
-- business logic (Task 20 Out of scope).

CREATE TABLE IF NOT EXISTS principals (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('human', 'service')),
    display     TEXT NOT NULL,
    idp_subject TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS organizations (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS org_members (
    org_id       TEXT NOT NULL REFERENCES organizations (id),
    principal_id TEXT NOT NULL REFERENCES principals (id),
    role         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, principal_id)
);

COMMENT ON TABLE principals IS 'Authoritative (Constitution C3, data-consistency.md §1): human/service identity records.';
COMMENT ON TABLE organizations IS 'Authoritative (Constitution C3, data-consistency.md §1): organization records.';
COMMENT ON TABLE org_members IS 'Authoritative (Constitution C3, data-consistency.md §1): principal-to-organization membership with role.';

-- +goose Down
DROP TABLE IF EXISTS org_members;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS principals;
