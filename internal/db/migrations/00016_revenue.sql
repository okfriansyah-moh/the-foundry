-- +goose Up
-- docs/PLAN.md Task 49 (VEN-10): Stripe event idempotency + revenue reconciliation.

CREATE TABLE IF NOT EXISTS stripe_events (
    id         TEXT PRIMARY KEY,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload    JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS revenue_reconciliation (
    id                TEXT PRIMARY KEY,
    at                TIMESTAMPTZ NOT NULL,
    subscriptions_usd NUMERIC(12,4) NOT NULL,
    refunds_usd       NUMERIC(12,4) NOT NULL,
    cancellations_usd NUMERIC(12,4) NOT NULL,
    discounts_usd     NUMERIC(12,4) NOT NULL,
    net_mrr_usd       NUMERIC(12,4) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS revenue_reconciliation;
DROP TABLE IF EXISTS stripe_events;
