package mission

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a lookup by ID finds no row.
var ErrNotFound = errors.New("mission: not found")

// Mission is a missions row: a mission's identity plus its parsed contract.
type Mission struct {
	ID          string
	PrincipalID string
	WorkflowID  string
	Contract    Contract
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// StateSnapshot is one mission_state row: an audit-trail record of one
// MissionLoop evaluator cycle (evaluator.go's Outcome/EvalState, flattened
// for persistence).
type StateSnapshot struct {
	MissionID        string
	Cycle            int
	NetMRRUSD        float64
	NoProgressCycles int
	Confirming       bool
	ConfirmedSince   *time.Time
	Status           string
	Reason           string
	ResultCode       string
	ObservedAt       time.Time
}

// GateEvent is one gate_events row: an unforeseen-human-gate escalation
// raised by a mission's loop (docs/PLAN.md Task 32's internal/recovery
// escalation pattern, applied here rather than reinvented).
type GateEvent struct {
	ID         string
	MissionID  string
	Action     string
	OccurredAt time.Time
	ResolvedAt *time.Time
	Resolution *string
}

// ReadinessArtifactRecord is one persisted mission-readiness artifact.
type ReadinessArtifactRecord struct {
	ID        string
	MissionID string
	Artifact  MissionReadinessArtifact
	CreatedAt time.Time
}

// LoopContract is one loop_contracts row: mission-contract.md §3's
// universal loop-contract fields
// ({trigger,cadence,authority,budget,metrics,exit}). Budget/Metrics are
// opaque JSON payloads (their shape varies per loop type across the eight
// loops mission-contract.md §3 names; this task only ever registers the
// mission loop's own contract).
type LoopContract struct {
	LoopName      string
	Trigger       string
	Cadence       string
	Authority     string
	Budget        json.RawMessage
	Metrics       json.RawMessage
	ExitCondition string
}

func newID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mission: generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
}

// Store is the Postgres-backed mission store
// (internal/db/migrations/00012_missions.sql).
type Store struct {
	db *sql.DB
}

// NewStore wraps an existing *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// CreateMission persists a new mission. m.ID must already be set by the
// caller (`foundry mission create` mints it).
func (s *Store) CreateMission(ctx context.Context, m Mission) error {
	contractJSON, err := json.Marshal(m.Contract)
	if err != nil {
		return fmt.Errorf("mission: encode contract for %s: %w", m.ID, err)
	}
	const q = `
INSERT INTO missions (id, principal_id, workflow_id, contract)
VALUES ($1, $2, $3, $4)`
	if _, err := s.db.ExecContext(ctx, q, m.ID, m.PrincipalID, m.WorkflowID, contractJSON); err != nil {
		return fmt.Errorf("mission: create %s: %w", m.ID, err)
	}
	return nil
}

// GetMission loads one mission by id.
func (s *Store) GetMission(ctx context.Context, id string) (Mission, error) {
	const q = `SELECT id, principal_id, workflow_id, contract, created_at, updated_at FROM missions WHERE id = $1`
	var m Mission
	var contractJSON []byte
	err := s.db.QueryRowContext(ctx, q, id).Scan(&m.ID, &m.PrincipalID, &m.WorkflowID, &contractJSON, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Mission{}, ErrNotFound
	}
	if err != nil {
		return Mission{}, fmt.Errorf("mission: get %s: %w", id, err)
	}
	if err := json.Unmarshal(contractJSON, &m.Contract); err != nil {
		return Mission{}, fmt.Errorf("mission: decode contract for %s: %w", id, err)
	}
	return m, nil
}

// RecordState appends one mission_state audit row.
func (s *Store) RecordState(ctx context.Context, snap StateSnapshot) error {
	id, err := newID("mstate")
	if err != nil {
		return err
	}
	const q = `
INSERT INTO mission_state (id, mission_id, cycle, net_mrr_usd, no_progress_cycles, confirming, confirmed_since, status, reason, result_code, observed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	if _, err := s.db.ExecContext(ctx, q, id, snap.MissionID, snap.Cycle, snap.NetMRRUSD, snap.NoProgressCycles, snap.Confirming, snap.ConfirmedSince, snap.Status, snap.Reason, snap.ResultCode, snap.ObservedAt); err != nil {
		return fmt.Errorf("mission: record state for %s: %w", snap.MissionID, err)
	}
	return nil
}

// RegisterLoopContract registers lc, idempotently: a second registration
// for the same LoopName is a no-op rather than an error (a mission's
// workflow may call this on every start/replay).
func (s *Store) RegisterLoopContract(ctx context.Context, lc LoopContract) error {
	id, err := newID("loopc")
	if err != nil {
		return err
	}
	const q = `
INSERT INTO loop_contracts (id, loop_name, trigger, cadence, authority, budget, metrics, exit_condition)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (loop_name) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, q, id, lc.LoopName, lc.Trigger, lc.Cadence, lc.Authority, []byte(lc.Budget), []byte(lc.Metrics), lc.ExitCondition); err != nil {
		return fmt.Errorf("mission: register loop contract %s: %w", lc.LoopName, err)
	}
	return nil
}

// HasLoopContract reports whether loopName has a registered loop_contracts
// row -- the check MissionLoop's RequireLoopContract activity uses to
// refuse to start without one.
func (s *Store) HasLoopContract(ctx context.Context, loopName string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM loop_contracts WHERE loop_name = $1)`
	var ok bool
	if err := s.db.QueryRowContext(ctx, q, loopName).Scan(&ok); err != nil {
		return false, fmt.Errorf("mission: check loop contract %s: %w", loopName, err)
	}
	return ok, nil
}

// RecordGateEvent persists a newly-raised gate event and returns its
// generated ID.
func (s *Store) RecordGateEvent(ctx context.Context, missionID, action string, occurredAt time.Time) (string, error) {
	id, err := newID("gate")
	if err != nil {
		return "", err
	}
	const q = `
INSERT INTO gate_events (id, mission_id, action, occurred_at)
VALUES ($1, $2, $3, $4)`
	if _, err := s.db.ExecContext(ctx, q, id, missionID, action, occurredAt); err != nil {
		return "", fmt.Errorf("mission: record gate event for %s: %w", missionID, err)
	}
	return id, nil
}

// ResolveGateEvent marks a previously-raised gate event resolved.
func (s *Store) ResolveGateEvent(ctx context.Context, id, resolution string, resolvedAt time.Time) error {
	const q = `UPDATE gate_events SET resolved_at = $2, resolution = $3 WHERE id = $1`
	res, err := s.db.ExecContext(ctx, q, id, resolvedAt, resolution)
	if err != nil {
		return fmt.Errorf("mission: resolve gate event %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mission: resolve gate event %s: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGateEvents returns a mission's gate events ordered by occurrence time.
func (s *Store) ListGateEvents(ctx context.Context, missionID string) ([]GateEvent, error) {
	const q = `
SELECT id, mission_id, action, occurred_at, resolved_at, resolution
FROM gate_events
WHERE mission_id = $1
ORDER BY occurred_at ASC`
	rows, err := s.db.QueryContext(ctx, q, missionID)
	if err != nil {
		return nil, fmt.Errorf("mission: list gate events for %s: %w", missionID, err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]GateEvent, 0)
	for rows.Next() {
		var ev GateEvent
		if err := rows.Scan(&ev.ID, &ev.MissionID, &ev.Action, &ev.OccurredAt, &ev.ResolvedAt, &ev.Resolution); err != nil {
			return nil, fmt.Errorf("mission: scan gate event for %s: %w", missionID, err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mission: iterate gate events for %s: %w", missionID, err)
	}
	return out, nil
}

// SaveReadinessArtifact persists one mission-readiness artifact.
func (s *Store) SaveReadinessArtifact(ctx context.Context, artifact MissionReadinessArtifact) error {
	id, err := newID("mready")
	if err != nil {
		return err
	}
	payload, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("mission: encode readiness artifact for %s: %w", artifact.MissionID, err)
	}
	const q = `
INSERT INTO mission_readiness_artifacts (id, mission_id, readiness, approved_by, digest, artifact)
VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := s.db.ExecContext(ctx, q, id, artifact.MissionID, artifact.Readiness, artifact.ApprovedBy, artifact.Digest, payload); err != nil {
		return fmt.Errorf("mission: save readiness artifact for %s: %w", artifact.MissionID, err)
	}
	return nil
}

// LatestReadinessArtifact returns the latest readiness artifact for missionID.
func (s *Store) LatestReadinessArtifact(ctx context.Context, missionID string) (ReadinessArtifactRecord, error) {
	const q = `
SELECT id, mission_id, artifact, created_at
FROM mission_readiness_artifacts
WHERE mission_id = $1
ORDER BY created_at DESC
LIMIT 1`
	var rec ReadinessArtifactRecord
	var payload []byte
	err := s.db.QueryRowContext(ctx, q, missionID).Scan(&rec.ID, &rec.MissionID, &payload, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadinessArtifactRecord{}, ErrNotFound
	}
	if err != nil {
		return ReadinessArtifactRecord{}, fmt.Errorf("mission: latest readiness artifact for %s: %w", missionID, err)
	}
	if err := json.Unmarshal(payload, &rec.Artifact); err != nil {
		return ReadinessArtifactRecord{}, fmt.Errorf("mission: decode readiness artifact for %s: %w", missionID, err)
	}
	return rec, nil
}

// HasPassingReadinessArtifact reports whether missionID has at least one
// readiness artifact with readiness=pass.
func (s *Store) HasPassingReadinessArtifact(ctx context.Context, missionID string) (bool, error) {
	const q = `
SELECT EXISTS(
  SELECT 1 FROM mission_readiness_artifacts
  WHERE mission_id = $1 AND readiness = $2
)`
	var ok bool
	if err := s.db.QueryRowContext(ctx, q, missionID, ReadinessPass).Scan(&ok); err != nil {
		return false, fmt.Errorf("mission: check readiness artifact for %s: %w", missionID, err)
	}
	return ok, nil
}

// MissionFilter bounds a ListMissions query. Status filters on the mission's
// latest recorded state; PrincipalID, when set, restricts results to that
// principal's own missions (docs/PLAN.md Task 118 / SEC-04 tenancy rule — a
// principal cannot list another's missions). Limit defaults to 50.
type MissionFilter struct {
	Status      string
	Profile     string
	PrincipalID string
	Limit       int
	Offset      int
}

// MissionListItem is a mission plus its latest recorded loop state, for the
// operator list/status surface.
type MissionListItem struct {
	Mission
	Status     string
	Reason     string
	Cycle      int
	ObservedAt *time.Time
}

// ListMissions returns missions with their latest loop state, newest first,
// with paging and an optional status filter (docs/PLAN.md Task 107).
func (s *Store) ListMissions(ctx context.Context, filter MissionFilter) ([]MissionListItem, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT m.id, m.principal_id, m.workflow_id, m.contract, m.created_at, m.updated_at,
       COALESCE(ms.status, ''), COALESCE(ms.reason, ''), COALESCE(ms.cycle, 0), ms.observed_at
FROM missions m
LEFT JOIN LATERAL (
    SELECT status, reason, cycle, observed_at
    FROM mission_state
    WHERE mission_id = m.id
    ORDER BY observed_at DESC, id DESC
    LIMIT 1
) ms ON true
WHERE ($1 = '' OR ms.status = $1)
  AND ($4 = '' OR m.principal_id = $4)
ORDER BY m.created_at DESC, m.id DESC
LIMIT $2 OFFSET $3`
	rows, err := s.db.QueryContext(ctx, q, filter.Status, limit, filter.Offset, filter.PrincipalID)
	if err != nil {
		return nil, fmt.Errorf("mission: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MissionListItem
	for rows.Next() {
		var it MissionListItem
		var contractJSON []byte
		var observedAt sql.NullTime
		if err := rows.Scan(&it.ID, &it.PrincipalID, &it.WorkflowID, &contractJSON, &it.CreatedAt, &it.UpdatedAt,
			&it.Status, &it.Reason, &it.Cycle, &observedAt); err != nil {
			return nil, fmt.Errorf("mission: scan list row: %w", err)
		}
		if err := json.Unmarshal(contractJSON, &it.Contract); err != nil {
			return nil, fmt.Errorf("mission: decode contract in list: %w", err)
		}
		if observedAt.Valid {
			t := observedAt.Time
			it.ObservedAt = &t
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mission: iterate list: %w", err)
	}
	return out, nil
}

// LatestState returns a mission's most recent recorded loop state, or
// ErrNotFound when the mission has produced no state yet.
func (s *Store) LatestState(ctx context.Context, missionID string) (StateSnapshot, error) {
	const q = `
SELECT mission_id, cycle, net_mrr_usd, no_progress_cycles, confirming, confirmed_since,
       status, reason, result_code, observed_at
FROM mission_state
WHERE mission_id = $1
ORDER BY observed_at DESC, id DESC
LIMIT 1`
	var snap StateSnapshot
	var confirmedSince sql.NullTime
	err := s.db.QueryRowContext(ctx, q, missionID).Scan(
		&snap.MissionID, &snap.Cycle, &snap.NetMRRUSD, &snap.NoProgressCycles, &snap.Confirming,
		&confirmedSince, &snap.Status, &snap.Reason, &snap.ResultCode, &snap.ObservedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StateSnapshot{}, ErrNotFound
	}
	if err != nil {
		return StateSnapshot{}, fmt.Errorf("mission: latest state for %s: %w", missionID, err)
	}
	if confirmedSince.Valid {
		snap.ConfirmedSince = &confirmedSince.Time
	}
	return snap, nil
}
