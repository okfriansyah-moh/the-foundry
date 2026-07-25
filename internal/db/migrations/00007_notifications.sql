-- +goose Up
-- docs/PLAN.md Task 20 (FND-01): outbound notification queue + delivery
-- state backing the Telegram engine (Task 30). This migration creates
-- shape only.

CREATE TABLE IF NOT EXISTS notifications (
    id         TEXT PRIMARY KEY,
    channel    TEXT NOT NULL,
    target     TEXT NOT NULL,
    class      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('pending', 'sent', 'failed', 'acked')) DEFAULT 'pending',
    attempts   INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS notifications_state_idx ON notifications (state);

COMMENT ON TABLE notifications IS 'Authoritative (Constitution C3/C11, data-consistency.md §1): outbound notification queue and delivery state.';

-- +goose Down
DROP TABLE IF EXISTS notifications;
