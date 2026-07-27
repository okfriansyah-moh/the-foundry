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
