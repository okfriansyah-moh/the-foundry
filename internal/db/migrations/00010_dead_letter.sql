-- +goose Up
-- docs/PLAN.md Task 33 (FND-14): general-purpose dead-letter table for
-- poisoned intake/queue work items, distinct from
-- 00007_notifications.sql's own notification-specific dead-letter path
-- (that one is Task 30's terminal 'failed' state on the notifications
-- table itself). This table has no such existing home: it is not
-- Telegram-specific and not covered by 00006_ledgers.sql's
-- external_operations lifecycle (that table exists to make a *side
-- effect* replay-safe; this one records a *work item* a lane's admission
-- logic gave up on before any side effect was attempted).

CREATE TABLE IF NOT EXISTS dead_letter_items (
    id         TEXT PRIMARY KEY,
    queue      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    reason     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS dead_letter_items_queue_idx ON dead_letter_items (queue);
CREATE INDEX IF NOT EXISTS dead_letter_items_created_at_idx ON dead_letter_items (created_at);

COMMENT ON TABLE dead_letter_items IS 'docs/PLAN.md Task 33 (FND-14): poisoned work items a bounded intake/priority-lane queue gave up on, alerted via the P1 notify path.';

-- +goose Down
DROP TABLE IF EXISTS dead_letter_items;
