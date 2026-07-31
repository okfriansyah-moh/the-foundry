package intake

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// PGStore is the Postgres-backed Store (internal/db/migrations/00026_intake.sql).
// Runs live in intake_runs; per-stage records live in the append-only
// intake_stages, unique per (run_id, stage) so a re-run is idempotent.
type PGStore struct {
	db *sql.DB
}

// NewPGStore wraps an existing *sql.DB.
func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

// CreateRun implements Store.
func (s *PGStore) CreateRun(ctx context.Context, r Run) (Run, error) {
	originJSON, err := json.Marshal(r.Origin)
	if err != nil {
		return Run{}, fmt.Errorf("intake: encode origin: %w", err)
	}
	const q = `
INSERT INTO intake_runs
  (id, idea, envelope_usd, research_cap_usd, mvp_cap_usd, origin,
   current_stage, status, spent_usd, mission_id, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	if _, err := s.db.ExecContext(ctx, q,
		r.ID, r.Idea, r.Caps.EnvelopeUSD, r.Caps.ResearchCapUSD, r.Caps.MVPCapUSD, originJSON,
		string(r.CurrentStage), string(r.Status), r.SpentUSD, nullStr(r.MissionID), r.CreatedAt, r.UpdatedAt,
	); err != nil {
		return Run{}, fmt.Errorf("intake: create run %s: %w", r.ID, err)
	}
	return r, nil
}

// GetRun implements Store.
func (s *PGStore) GetRun(ctx context.Context, runID string) (Run, error) {
	const q = `
SELECT id, idea, envelope_usd, research_cap_usd, mvp_cap_usd, origin,
       current_stage, status, spent_usd, COALESCE(mission_id,''), created_at, updated_at
FROM intake_runs WHERE id = $1`
	return s.scanRun(s.db.QueryRowContext(ctx, q, runID))
}

// ListRuns implements Store.
func (s *PGStore) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, idea, envelope_usd, research_cap_usd, mvp_cap_usd, origin,
       current_stage, status, spent_usd, COALESCE(mission_id,''), created_at, updated_at
FROM intake_runs ORDER BY created_at DESC, id DESC LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("intake: list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Run
	for rows.Next() {
		r, err := s.scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intake: iterate runs: %w", err)
	}
	return out, nil
}

// UpdateRun implements Store.
func (s *PGStore) UpdateRun(ctx context.Context, r Run) error {
	const q = `
UPDATE intake_runs
SET current_stage=$2, status=$3, spent_usd=$4, mission_id=$5, updated_at=$6
WHERE id=$1`
	res, err := s.db.ExecContext(ctx, q,
		r.ID, string(r.CurrentStage), string(r.Status), r.SpentUSD, nullStr(r.MissionID), r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("intake: update run %s: %w", r.ID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRunNotFound
	}
	return nil
}

// RecordStage implements Store. ON CONFLICT DO NOTHING makes a duplicate
// (run, stage) a silent no-op, so replay never double-records.
func (s *PGStore) RecordStage(ctx context.Context, rec StageRecord) error {
	const q = `
INSERT INTO intake_stages (run_id, stage, input_digest, output, cost_usd, created_at)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (run_id, stage) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, q,
		rec.RunID, string(rec.Stage), rec.InputDigest, rec.Output, rec.CostUSD, rec.CreatedAt); err != nil {
		return fmt.Errorf("intake: record stage %s/%s: %w", rec.RunID, rec.Stage, err)
	}
	return nil
}

// GetStage implements Store.
func (s *PGStore) GetStage(ctx context.Context, runID string, stage Stage) (StageRecord, bool, error) {
	const q = `
SELECT run_id, stage, input_digest, output, cost_usd, created_at
FROM intake_stages WHERE run_id=$1 AND stage=$2`
	var rec StageRecord
	var st string
	err := s.db.QueryRowContext(ctx, q, runID, string(stage)).
		Scan(&rec.RunID, &st, &rec.InputDigest, &rec.Output, &rec.CostUSD, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StageRecord{}, false, nil
	}
	if err != nil {
		return StageRecord{}, false, fmt.Errorf("intake: get stage %s/%s: %w", runID, stage, err)
	}
	rec.Stage = Stage(st)
	return rec, true, nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for scanRun.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *PGStore) scanRun(row rowScanner) (Run, error) {
	var r Run
	var stage, status string
	var originJSON []byte
	var created, updated time.Time
	err := row.Scan(&r.ID, &r.Idea, &r.Caps.EnvelopeUSD, &r.Caps.ResearchCapUSD, &r.Caps.MVPCapUSD,
		&originJSON, &stage, &status, &r.SpentUSD, &r.MissionID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("intake: scan run: %w", err)
	}
	if len(originJSON) > 0 {
		if err := json.Unmarshal(originJSON, &r.Origin); err != nil {
			return Run{}, fmt.Errorf("intake: decode origin: %w", err)
		}
	}
	r.CurrentStage = Stage(stage)
	r.Status = Status(status)
	r.CreatedAt = created
	r.UpdatedAt = updated
	return r, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
