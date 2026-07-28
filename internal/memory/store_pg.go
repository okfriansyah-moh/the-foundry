package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// PGStore is the Postgres-backed Store (internal/db/migrations/00024_memory).
// It is the durable production implementation; MemStore is its in-memory
// analog for tests. Both honor identical semantics: profile-scoped retrieval
// and source-cascade deletion.
type PGStore struct {
	db *sql.DB
}

// NewPGStore wraps an existing *sql.DB.
func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

// Put implements Store. Memory rows are upserted by ID; evidence refs are
// rewritten to match the memory's current ref set.
func (s *PGStore) Put(ctx context.Context, m Memory) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var expires any
	if !m.ExpiresAt.IsZero() {
		expires = m.ExpiresAt.UTC()
	}
	const upsert = `
INSERT INTO memories (id, content, kind, profile_scope, confidence, ttl_seconds, created_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (id) DO UPDATE SET
  content = EXCLUDED.content, kind = EXCLUDED.kind, confidence = EXCLUDED.confidence,
  ttl_seconds = EXCLUDED.ttl_seconds, expires_at = EXCLUDED.expires_at`
	if _, err := tx.ExecContext(ctx, upsert, m.ID, m.Content, m.Kind, m.ProfileScope,
		m.Confidence, int64(m.TTL/time.Second), m.CreatedAt.UTC(), expires); err != nil {
		return fmt.Errorf("memory: upsert memory: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_evidence WHERE memory_id = $1`, m.ID); err != nil {
		return fmt.Errorf("memory: clear evidence refs: %w", err)
	}
	for _, ref := range m.EvidenceRefs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_evidence (memory_id, evidence_ref) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			m.ID, ref); err != nil {
			return fmt.Errorf("memory: insert evidence ref: %w", err)
		}
	}
	return tx.Commit()
}

// GetForProfile implements Store — profile-scoped, no cross-profile read.
func (s *PGStore) GetForProfile(ctx context.Context, profile, id string) (Memory, bool, error) {
	const q = `SELECT content, kind, confidence, ttl_seconds, created_at, expires_at
FROM memories WHERE id = $1 AND profile_scope = $2`
	var m Memory
	var ttlSec int64
	var expires sql.NullTime
	err := s.db.QueryRowContext(ctx, q, id, profile).Scan(&m.Content, &m.Kind, &m.Confidence, &ttlSec, &m.CreatedAt, &expires)
	if err == sql.ErrNoRows {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, fmt.Errorf("memory: get %s: %w", id, err)
	}
	m.ID = id
	m.ProfileScope = profile
	m.TTL = time.Duration(ttlSec) * time.Second
	if expires.Valid {
		m.ExpiresAt = expires.Time
	}
	refs, err := s.refsFor(ctx, id)
	if err != nil {
		return Memory{}, false, err
	}
	m.EvidenceRefs = refs
	return m, true, nil
}

// ListByProfile implements Store.
func (s *PGStore) ListByProfile(ctx context.Context, profile string) ([]Memory, error) {
	const q = `SELECT id, content, kind, confidence, ttl_seconds, created_at, expires_at
FROM memories WHERE profile_scope = $1 ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, profile)
	if err != nil {
		return nil, fmt.Errorf("memory: list %s: %w", profile, err)
	}
	defer func() { _ = rows.Close() }()
	var out []Memory
	for rows.Next() {
		var m Memory
		var ttlSec int64
		var expires sql.NullTime
		if err := rows.Scan(&m.ID, &m.Content, &m.Kind, &m.Confidence, &ttlSec, &m.CreatedAt, &expires); err != nil {
			return nil, fmt.Errorf("memory: scan: %w", err)
		}
		m.ProfileScope = profile
		m.TTL = time.Duration(ttlSec) * time.Second
		if expires.Valid {
			m.ExpiresAt = expires.Time
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		refs, err := s.refsFor(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].EvidenceRefs = refs
	}
	return out, nil
}

// DeleteBySource implements Store. Deleting the memory rows cascades to
// memory_evidence and memory_vectors via ON DELETE CASCADE (00024_memory.sql).
func (s *PGStore) DeleteBySource(ctx context.Context, ref string) ([]string, error) {
	const q = `
DELETE FROM memories
WHERE id IN (SELECT memory_id FROM memory_evidence WHERE evidence_ref = $1)
RETURNING id`
	rows, err := s.db.QueryContext(ctx, q, ref)
	if err != nil {
		return nil, fmt.Errorf("memory: delete by source %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()
	var deleted []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("memory: scan deleted id: %w", err)
		}
		deleted = append(deleted, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(deleted)
	return deleted, nil
}

func (s *PGStore) refsFor(ctx context.Context, memoryID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT evidence_ref FROM memory_evidence WHERE memory_id = $1 ORDER BY evidence_ref`, memoryID)
	if err != nil {
		return nil, fmt.Errorf("memory: read refs for %s: %w", memoryID, err)
	}
	defer func() { _ = rows.Close() }()
	var refs []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}
