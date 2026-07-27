-- +goose Up
-- docs/PLAN.md Task 40 (VEN-01): MissionContract engine + result codes
-- (Constitution C18 -- missions are formal, bounded contracts, never open
-- loops; docs/foundry/docs/autonomy/mission-contract.md). Owned by
-- internal/mission (go-kernel authority per Constitution C4: mission
-- evaluation drives kernel-owned orchestration). This migration creates
-- shape only, no business logic.
--
-- decision (no-gaps rule, documented per this task's Status line): the
-- card's Steps name migration "0009_missions.sql", but
-- internal/db/migrations/00009_budgets.sql (Task 29) and 00010/00011
-- (Tasks 34/38) already occupy 00009-00011 by the time this task runs --
-- the card's literal number is stale relative to actual migration
-- history. 00012 is the next unused sequential number as of this
-- session; the smallest reversible fix is to renumber, not to leave a
-- collision.

CREATE TABLE IF NOT EXISTS missions (
    id          TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    workflow_id TEXT NOT NULL UNIQUE, -- the MissionLoop Temporal workflow ID this mission's contract drives
    contract    JSONB NOT NULL,       -- the parsed MissionContract (mission-contract.md §1), as validated JSON
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- mission_state is an append-only audit trail of MissionLoop's own
-- evaluator cycles (internal/mission.EvalState snapshots) -- one row per
-- observe-cadence cycle, mirroring workflow_transitions' append-only
-- shape but scoped to mission-internal evaluator progress rather than the
-- canonical state.Transition stream (which missions also emit, via the
-- shared workflow_transitions table, keyed by their own workflow_id).
CREATE TABLE IF NOT EXISTS mission_state (
    id                TEXT PRIMARY KEY,
    mission_id        TEXT NOT NULL REFERENCES missions (id),
    cycle             INTEGER NOT NULL,
    net_mrr_usd       NUMERIC(12, 4) NOT NULL,
    no_progress_cycles INTEGER NOT NULL,
    confirming        BOOLEAN NOT NULL,
    confirmed_since   TIMESTAMPTZ,
    status            TEXT NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    result_code       TEXT NOT NULL DEFAULT '',
    observed_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS mission_state_mission_id_idx ON mission_state (mission_id, cycle);

-- gate_events records every unforeseen-human-gate escalation a mission's
-- loop raises (docs/PLAN.md Task 32's internal/recovery escalation
-- pattern, applied here rather than reinvented): one row per gate raised,
-- resolved_at/resolution set once an operator clears it via `foundry
-- mission pause`-driven resume.
CREATE TABLE IF NOT EXISTS gate_events (
    id           TEXT PRIMARY KEY,
    mission_id   TEXT NOT NULL REFERENCES missions (id),
    action       TEXT NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ,
    resolution   TEXT
);

CREATE INDEX IF NOT EXISTS gate_events_mission_id_idx ON gate_events (mission_id);

-- loop_contracts: mission-contract.md §3's universal loop contract --
-- every loop (mission, delivery, recovery, ...) MUST register
-- {trigger,cadence,authority,budget,metrics,exit} before it may run.
-- MissionLoop's RequireLoopContract activity refuses to start the
-- workflow when no row exists for its loop_name (fitlint's "missionloop"
-- check proves this call is structurally present in workflow.go).
CREATE TABLE IF NOT EXISTS loop_contracts (
    id             TEXT PRIMARY KEY,
    loop_name      TEXT NOT NULL UNIQUE, -- e.g. "mission:<mission_id>"
    trigger        TEXT NOT NULL,
    cadence        TEXT NOT NULL,
    authority      TEXT NOT NULL,
    budget         JSONB NOT NULL,
    metrics        JSONB NOT NULL,
    exit_condition TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE missions IS 'Authoritative (Constitution C3/C18, data-consistency.md §1): mission contract records.';
COMMENT ON TABLE mission_state IS 'Authoritative append-only audit trail (Constitution C18): MissionLoop evaluator-cycle snapshots.';
COMMENT ON TABLE gate_events IS 'Authoritative (Constitution C22): mission unforeseen-human-gate escalation records.';
COMMENT ON TABLE loop_contracts IS 'Authoritative (mission-contract.md §3): universal loop-contract registrations; MissionLoop refuses to start without one.';

-- +goose Down
DROP TABLE IF EXISTS loop_contracts;
DROP TABLE IF EXISTS gate_events;
DROP TABLE IF EXISTS mission_state;
DROP TABLE IF EXISTS missions;
