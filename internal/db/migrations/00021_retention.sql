-- docs/PLAN.md Task 66 (HRD-03) — retention holds and DSR requests.
-- +goose Up
CREATE TABLE IF NOT EXISTS legal_holds (
    id BIGSERIAL PRIMARY KEY,
    subject_key TEXT NOT NULL UNIQUE,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS dsr_requests (
    id BIGSERIAL PRIMARY KEY,
    principal TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose Down
DROP TABLE IF EXISTS dsr_requests;
DROP TABLE IF EXISTS legal_holds;
