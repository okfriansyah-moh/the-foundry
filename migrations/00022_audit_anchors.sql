-- docs/PLAN.md Task 67 (HRD-04) — audit checkpoint anchors.
-- +goose Up
CREATE TABLE IF NOT EXISTS audit_anchors (
    id BIGSERIAL PRIMARY KEY,
    start_seq BIGINT NOT NULL,
    end_seq BIGINT NOT NULL,
    digest TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose Down
DROP TABLE IF EXISTS audit_anchors;
