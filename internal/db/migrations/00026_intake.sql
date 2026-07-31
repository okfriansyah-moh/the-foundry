-- +goose Up
-- Task 111 (INT-03): staged, resumable idea-intake pipeline persistence.
-- intake_runs holds one row per `foundry mission start --idea` run; intake_stages
-- is the append-only, per-stage record whose (run_id, stage) uniqueness makes a
-- stage re-run idempotent (it never re-charges the budget or re-calls a provider).

CREATE TABLE IF NOT EXISTS intake_runs (
    id               TEXT PRIMARY KEY,
    idea             TEXT NOT NULL,
    envelope_usd     DOUBLE PRECISION NOT NULL DEFAULT 0,
    research_cap_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    mvp_cap_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    origin           JSONB NOT NULL DEFAULT '{}'::jsonb,
    current_stage    TEXT NOT NULL,
    status           TEXT NOT NULL,
    spent_usd        DOUBLE PRECISION NOT NULL DEFAULT 0,
    mission_id       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS intake_runs_created_idx
    ON intake_runs (created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS intake_stages (
    run_id       TEXT NOT NULL REFERENCES intake_runs(id),
    stage        TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    output       BYTEA NOT NULL,
    cost_usd     DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, stage)
);

-- +goose Down
DROP TABLE IF EXISTS intake_stages;
DROP TABLE IF EXISTS intake_runs;
