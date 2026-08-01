-- +goose Up
-- docs/PLAN.md Task 126 (VEN-16): durable Stripe webhook metadata and reconciled revenue provenance.

ALTER TABLE stripe_events
    ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS livemode BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS stripe_created_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ;

ALTER TABLE revenue_reconciliation
    ADD COLUMN IF NOT EXISTS stripe_balance_transaction_id TEXT,
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'stripe_test_mode';

CREATE UNIQUE INDEX IF NOT EXISTS revenue_reconciliation_stripe_balance_transaction_id_key
    ON revenue_reconciliation (stripe_balance_transaction_id)
    WHERE stripe_balance_transaction_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS revenue_reconciliation_stripe_balance_transaction_id_key;

ALTER TABLE revenue_reconciliation
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS stripe_balance_transaction_id;

ALTER TABLE stripe_events
    DROP COLUMN IF EXISTS processed_at,
    DROP COLUMN IF EXISTS stripe_created_at,
    DROP COLUMN IF EXISTS livemode,
    DROP COLUMN IF EXISTS type;
