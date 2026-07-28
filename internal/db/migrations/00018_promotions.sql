-- +goose Up
-- docs/PLAN.md Task 51 (VEN-12): promotions table — one row per bounded
-- autonomous improvement cycle that completes a plan-cycle push. Records
-- the change reference, plan digest, before/after metric snapshots, rollback
-- reference, and cycle level. The improvement lease column serialises
-- concurrent improvement attempts: at most one in-flight per product.

CREATE TABLE IF NOT EXISTS promotions (
    id              TEXT PRIMARY KEY,
    mission_id      TEXT NOT NULL REFERENCES missions (id),
    product_id      TEXT NOT NULL,
    change_ref      TEXT NOT NULL,         -- git SHA / deploy ref of the promoted artifact
    plan_digest     TEXT NOT NULL,         -- SHA-256 of the generated improvement PLAN
    metrics_before  JSONB NOT NULL DEFAULT '{}',
    metrics_after   JSONB NOT NULL DEFAULT '{}',
    rollback_ref    TEXT NOT NULL,         -- ref to revert to (Task 52 veto path)
    level           TEXT NOT NULL DEFAULT 'plan-cycle',
    vetoed          BOOLEAN NOT NULL DEFAULT FALSE,
    vetoed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS promotions_mission_idx
    ON promotions (mission_id, created_at DESC);
CREATE INDEX IF NOT EXISTS promotions_product_idx
    ON promotions (product_id, created_at DESC);

-- improvement_leases serialises at most one in-flight improvement per
-- product. The lease is held for the duration of the improvement cycle;
-- releasing it (DELETE) or failing to renew allows the next cycle to start.
CREATE TABLE IF NOT EXISTS improvement_leases (
    product_id  TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions (id),
    lease_id    TEXT NOT NULL UNIQUE,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS improvement_leases;
DROP TABLE IF EXISTS promotions;
