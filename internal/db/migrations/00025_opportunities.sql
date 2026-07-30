-- +goose Up
-- docs/PLAN.md Task 100 (OPP-01): storable, deterministic opportunity model.
-- Four tables: the opportunity itself, its append-only evidence log, the
-- deterministic scorecards computed over that evidence, and the verdicts bound
-- to the exact scorecard/thresholds/config version that produced them.

CREATE TABLE IF NOT EXISTS opportunities (
    id                            TEXT PRIMARY KEY,
    statement                     TEXT NOT NULL,
    submitted_by                  TEXT NOT NULL DEFAULT '',
    submitted_at                  TIMESTAMPTZ,
    source                        TEXT NOT NULL DEFAULT '',
    icp                           JSONB NOT NULL DEFAULT '{}'::jsonb,
    estimated_validation_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    mvp_budget_usd                DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_active_builds             INTEGER NOT NULL DEFAULT 1,
    real_validation_signal        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Append-only evidence log. seq gives a stable append order; there is no
-- update or delete path, so an opportunity's evidence trail cannot be
-- rewritten (Task 100 Step 6).
CREATE TABLE IF NOT EXISTS opportunity_evidence (
    id             TEXT PRIMARY KEY,
    opportunity_id TEXT NOT NULL REFERENCES opportunities(id),
    seq            BIGSERIAL,
    kind           TEXT NOT NULL,
    text           TEXT NOT NULL,
    label          TEXT NOT NULL,
    basis          TEXT NOT NULL DEFAULT '',
    source_ref     TEXT NOT NULL DEFAULT '',
    observed_at    TIMESTAMPTZ,
    untrusted      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS opportunity_evidence_opp_idx
    ON opportunity_evidence (opportunity_id, seq ASC);

CREATE TABLE IF NOT EXISTS opportunity_scores (
    id               TEXT PRIMARY KEY,
    opportunity_id   TEXT NOT NULL REFERENCES opportunities(id),
    scorecard        JSONB NOT NULL,
    scorecard_digest TEXT NOT NULL,
    config_version   TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS opportunity_scores_opp_idx
    ON opportunity_scores (opportunity_id, created_at DESC);

CREATE TABLE IF NOT EXISTS opportunity_verdicts (
    id                TEXT PRIMARY KEY,
    opportunity_id    TEXT NOT NULL REFERENCES opportunities(id),
    verdict           TEXT NOT NULL,
    unmet_thresholds  JSONB NOT NULL DEFAULT '[]'::jsonb,
    scorecard_digest  TEXT NOT NULL,
    thresholds_digest TEXT NOT NULL,
    config_version    TEXT NOT NULL,
    scorecard         JSONB NOT NULL,
    thresholds        JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS opportunity_verdicts_opp_idx
    ON opportunity_verdicts (opportunity_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS opportunity_verdicts;
DROP TABLE IF EXISTS opportunity_scores;
DROP TABLE IF EXISTS opportunity_evidence;
DROP TABLE IF EXISTS opportunities;
