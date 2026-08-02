-- +goose Up
-- docs/PLAN.md Task 150 (INT-09): unified input router durable requests.
CREATE TABLE IF NOT EXISTS input_router_requests (
    request_id          TEXT PRIMARY KEY,
    idempotency_key     TEXT NOT NULL DEFAULT '',
    kind                TEXT NOT NULL CHECK (kind IN ('IDEA','PLAN','MOCKUP')),
    origin              TEXT NOT NULL CHECK (origin IN ('CLI','TELEGRAM','API')),
    principal_id        TEXT NOT NULL,
    profile_id          TEXT NOT NULL DEFAULT '',
    organization_id     TEXT NOT NULL DEFAULT '',
    mode                TEXT NOT NULL DEFAULT '',
    text_hash           TEXT NOT NULL DEFAULT '',
    artifact_bundle_digest TEXT NOT NULL DEFAULT '',
    plan_ref            TEXT NOT NULL DEFAULT '',
    budget_usd          DOUBLE PRECISION NOT NULL DEFAULT 0,
    route_decision      TEXT NOT NULL DEFAULT '',
    downstream_ref      TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS input_router_idem_uidx
    ON input_router_requests (idempotency_key)
    WHERE idempotency_key <> '';

-- +goose Down
DROP INDEX IF EXISTS input_router_idem_uidx;
DROP TABLE IF EXISTS input_router_requests;
