package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
)

// AppendAuditRow appends one row to the audit_log hash chain
// (internal/db/migrations/00008_audit.sql). Task 20 (FND-01) shipped that
// table's shape only ("chain computed by a trigger-free Go writer, not a
// DB trigger" per the migration's own comment) but never shipped the Go
// writer itself. decision (no-gaps rule): rather than inventing a
// general-purpose internal/audit package out of this task's scope, this
// is the smallest reversible writer needed for `foundry plan revoke`'s
// own audit row — hash = sha256(prev_hash || payload), chained off the
// highest-seq row currently in the table. A future task can extract a
// shared writer without changing audit_log's contract.
//
// decision (Task 39 / FND-20 fix, found live against a real Postgres while
// building VerifyAuditChain): payload is stored as JSONB, and Postgres
// rewrites JSON text into its own canonical form on write (e.g. `{"n":1}`
// is read back as `{"n": 1}`) — hashing the caller's pre-insert bytes and
// later re-hashing the bytes read back from the column can NEVER match,
// which would make every single row look "tampered" even when nothing
// touched it. Fixed by asking Postgres to canonicalize payload first
// (`SELECT $1::jsonb`, a pure read-only cast with no side effect) and
// hashing/storing that same canonical form, so the bytes VerifyAuditChain
// re-reads later are byte-identical to the bytes AppendAuditRow hashed.
func AppendAuditRow(ctx context.Context, db *sql.DB, actor, action, subject string, payload []byte) error {
	var canonicalPayload []byte
	if err := db.QueryRowContext(ctx, `SELECT $1::jsonb`, payload).Scan(&canonicalPayload); err != nil {
		return fmt.Errorf("provenance: canonicalize audit payload: %w", err)
	}

	var prevHash []byte
	err := db.QueryRowContext(ctx, `SELECT hash FROM audit_log ORDER BY seq DESC LIMIT 1`).Scan(&prevHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("provenance: read last audit_log hash: %w", err)
	}

	h := sha256.New()
	h.Write(prevHash)
	h.Write(canonicalPayload)
	hash := h.Sum(nil)

	const q = `INSERT INTO audit_log (actor, action, subject, payload, prev_hash, hash) VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := db.ExecContext(ctx, q, actor, action, subject, canonicalPayload, prevHash, hash); err != nil {
		return fmt.Errorf("provenance: insert audit_log row: %w", err)
	}
	return nil
}

// AuditVerifyResult is the outcome of VerifyAuditChain: the full audit_log
// hash chain re-derived and compared row-by-row against what is actually
// stored (docs/PLAN.md Task 39 / FND-20 M1-exit Acceptance: "audit chain
// verify — writer from migration 0008 + `foundry audit verify`").
type AuditVerifyResult struct {
	RowCount int
	OK       bool
	// BadSeq is the seq of the first row whose stored hash does not match
	// sha256(prev_hash || payload) recomputed from that same row, or 0 if
	// OK is true. A mismatch here means the row's own payload/hash pair was
	// tampered with in place.
	BadSeq int64
	// BrokenLinkSeq is the seq of the first row whose stored prev_hash does
	// not equal the immediately preceding row's stored hash, or 0 if OK is
	// true. A mismatch here means a row was deleted, inserted out of band,
	// or reordered — the row itself may be internally hash-consistent but
	// no longer chained to its real predecessor.
	BrokenLinkSeq int64
}

// VerifyAuditChain re-derives the audit_log hash chain from seq 1 onward
// and reports whether every row's stored hash matches
// sha256(prev_hash || payload) recomputed from that row's own stored
// prev_hash and payload, AND that every row's prev_hash matches the
// previous row's stored hash (seq order, no gaps assumed — BIGSERIAL rows
// are read in seq ASC order, which is the chain's own definition).
//
// This is a read-only verification pass: it never writes, and it treats
// every value read from audit_log as untrusted input to be recomputed and
// compared, never assumed correct (ai-vulnerability-defense: this is the
// one function in the repo whose entire job is to NOT trust stored data at
// face value). An empty table (zero rows) is valid and reports OK with
// RowCount 0 — verifying a chain that was never written to is not the same
// claim as verifying a non-empty chain, and callers should check RowCount
// when that distinction matters (as `foundry audit verify` and the M1-exit
// drill both do).
func VerifyAuditChain(ctx context.Context, db *sql.DB) (*AuditVerifyResult, error) {
	rows, err := db.QueryContext(ctx, `SELECT seq, payload, prev_hash, hash FROM audit_log ORDER BY seq ASC`)
	if err != nil {
		return nil, fmt.Errorf("provenance: query audit_log: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := &AuditVerifyResult{OK: true}
	var prevHash []byte
	for rows.Next() {
		var (
			seq            int64
			payload        []byte
			storedPrevHash []byte
			storedHash     []byte
		)
		if err := rows.Scan(&seq, &payload, &storedPrevHash, &storedHash); err != nil {
			return nil, fmt.Errorf("provenance: scan audit_log row: %w", err)
		}
		result.RowCount++

		if result.OK && result.RowCount > 1 && !bytes.Equal(storedPrevHash, prevHash) {
			result.OK = false
			result.BrokenLinkSeq = seq
		}

		h := sha256.New()
		h.Write(storedPrevHash)
		h.Write(payload)
		recomputed := h.Sum(nil)
		if result.OK && !bytes.Equal(recomputed, storedHash) {
			result.OK = false
			result.BadSeq = seq
		}

		prevHash = storedHash
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("provenance: iterate audit_log rows: %w", err)
	}
	return result, nil
}
