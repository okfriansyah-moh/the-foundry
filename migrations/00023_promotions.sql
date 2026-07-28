-- docs/PLAN.md Task 74 (EVO-01) — promotion candidate metadata.
-- +goose Up
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS tunable_name TEXT;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS previous_value DOUBLE PRECISION;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS promoted_value DOUBLE PRECISION;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS pipeline_stage TEXT;
-- +goose Down
ALTER TABLE promotions DROP COLUMN IF EXISTS pipeline_stage;
ALTER TABLE promotions DROP COLUMN IF EXISTS promoted_value;
ALTER TABLE promotions DROP COLUMN IF EXISTS previous_value;
ALTER TABLE promotions DROP COLUMN IF EXISTS tunable_name;
