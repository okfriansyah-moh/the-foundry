-- +goose Up
-- Task 112 (INT-04): durable inbound Telegram transport.
--   * next_attempt_at makes retry/backoff pacing (incl. Telegram's authoritative
--     retry_after) survive a daemon restart instead of every pending row becoming
--     immediately eligible.
--   * telegram_offsets records the getUpdates offset per bot so a restart resumes
--     exactly where it stopped — no re-delivery, no gap.
--   * notification_batch_windows persists un-flushed coalescing windows so a
--     restart mid-window does not silently drop a batched notification.

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS notifications_pending_ready_idx
    ON notifications (created_at ASC)
    WHERE state = 'pending';

CREATE TABLE IF NOT EXISTS telegram_offsets (
    bot_id         TEXT PRIMARY KEY,
    last_update_id BIGINT NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_batch_windows (
    digest_key        TEXT PRIMARY KEY,
    payload           JSONB NOT NULL,
    window_expires_at TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS notification_batch_windows_expiry_idx
    ON notification_batch_windows (window_expires_at ASC);

-- +goose Down
DROP TABLE IF EXISTS notification_batch_windows;
DROP TABLE IF EXISTS telegram_offsets;
DROP INDEX IF EXISTS notifications_pending_ready_idx;
ALTER TABLE notifications DROP COLUMN IF EXISTS next_attempt_at;
