-- +goose Up
-- Task 127 (VEN-17): durable improvement freeze state. internal/evolve/budget.go
-- held the promotion freeze in a process-global atomic.Bool, so a freeze set by
-- foundryd was invisible to the `foundry` CLI and evaporated on restart. Moving
-- it to Postgres makes a change-budget breach freeze promotion durably: the
-- freeze is visible cross-process and survives a `kill -9`, and
-- `foundry promotions unfreeze` genuinely clears it (and deletes the
-- improvement_leases row, already defined in 00018) with an audit_log entry —
-- making that command's own doc comment true instead of false.

CREATE TABLE IF NOT EXISTS improvement_freeze (
    scope     TEXT PRIMARY KEY,   -- 'global' for the daemon-wide latch, or a product_id
    reason    TEXT NOT NULL,
    frozen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE improvement_freeze IS 'Authoritative (docs/PLAN.md Task 127, Constitution C20): durable promotion-freeze latch. A row means promotion is frozen for that scope; its absence means unfrozen. Survives a foundryd restart so a change-budget breach cannot be silently cleared by a process bounce.';

-- +goose Down
DROP TABLE IF EXISTS improvement_freeze;
