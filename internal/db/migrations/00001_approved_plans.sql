-- +goose Up
-- docs/PLAN.md Task 8 (SKP-06): approved_plans is the persisted terminal
-- artifact of the provenance chain (Constitution C7). data holds the full
-- signed ApprovedPlan JSON (internal/provenance.ApprovedPlan's
-- MarshalJSON), including the Ed25519 signature; plan_digest/approved_at/
-- expires_at/revoked are duplicated as plain columns for indexing/queries
-- without needing to unpack the JSON blob. Ported byte-for-semantically-
-- identical from migrations/0001_approved_plans.sql (raw-SQL era) into
-- goose format by Task 20 (FND-01) — schema itself is unchanged.
CREATE TABLE IF NOT EXISTS approved_plans (
    plan_id      TEXT PRIMARY KEY,
    plan_digest  TEXT NOT NULL,
    approved_at  TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    data         JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS approved_plans_plan_digest_idx ON approved_plans (plan_digest);

COMMENT ON TABLE approved_plans IS 'Authoritative (Constitution C3/C7): the persisted signed ApprovedPlan provenance chain terminal artifact. Not a projection.';

-- +goose Down
DROP TABLE IF EXISTS approved_plans;
