package bench

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PostgresStore persists RunRecords to the benchmark_runs table.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore returns a DB-backed store.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

// Save upserts a run record.
func (s *PostgresStore) Save(ctx context.Context, record *RunRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("bench: marshal record for db: %w", err)
	}
	const q = `
INSERT INTO benchmark_runs (id, arm, work_item_id, recorded_at, environment_digest, payload)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)
ON CONFLICT (id) DO UPDATE SET
  arm = EXCLUDED.arm,
  work_item_id = EXCLUDED.work_item_id,
  recorded_at = EXCLUDED.recorded_at,
  environment_digest = EXCLUDED.environment_digest,
  payload = EXCLUDED.payload`
	_, err = s.db.ExecContext(ctx, q,
		record.ID,
		string(record.Arm),
		record.WorkItemID,
		record.RecordedAt.UTC(),
		record.EnvironmentDigest,
		payload,
	)
	if err != nil {
		return fmt.Errorf("bench: save record %s: %w", record.ID, err)
	}
	return nil
}

// Load reads a record by id.
func (s *PostgresStore) Load(ctx context.Context, id string) (*RunRecord, error) {
	const q = `SELECT payload FROM benchmark_runs WHERE id = $1`
	var raw []byte
	if err := s.db.QueryRowContext(ctx, q, id).Scan(&raw); err != nil {
		return nil, fmt.Errorf("bench: load record %s: %w", id, err)
	}
	var r RunRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("bench: decode record %s: %w", id, err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListByArm returns all records for an arm ordered by recorded_at.
func (s *PostgresStore) ListByArm(ctx context.Context, arm Arm) ([]*RunRecord, error) {
	const q = `SELECT payload FROM benchmark_runs WHERE arm = $1 ORDER BY recorded_at`
	rows, err := s.db.QueryContext(ctx, q, string(arm))
	if err != nil {
		return nil, fmt.Errorf("bench: list arm %s: %w", arm, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*RunRecord
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("bench: scan payload: %w", err)
		}
		var r RunRecord
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("bench: decode list payload: %w", err)
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// Delete removes a record (used in tests).
func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM benchmark_runs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("bench: delete %s: %w", id, err)
	}
	return nil
}

// TableExists reports whether benchmark_runs is present (migration applied).
func TableExists(ctx context.Context, db *sql.DB) (bool, error) {
	const q = `SELECT to_regclass('public.benchmark_runs') IS NOT NULL`
	var ok bool
	if err := db.QueryRowContext(ctx, q).Scan(&ok); err != nil {
		return false, fmt.Errorf("bench: table exists: %w", err)
	}
	return ok, nil
}

// SyncFileToDB copies all file-store records into Postgres.
func SyncFileToDB(ctx context.Context, files *FileStore, pg *PostgresStore) (int, error) {
	recs, err := files.LoadAll()
	if err != nil {
		return 0, err
	}
	for _, r := range recs {
		if err := pg.Save(ctx, r); err != nil {
			return 0, err
		}
	}
	return len(recs), nil
}

// NowUTC is a test seam for timestamps.
var NowUTC = func() time.Time { return time.Now().UTC() }
