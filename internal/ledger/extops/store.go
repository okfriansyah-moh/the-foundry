package extops

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// OpID identifies one row of external_operations.
type OpID string

// State is one of the four external_operations.state values enforced by
// internal/db/migrations/00006_ledgers.sql's CHECK constraint.
//
// decision: this task (docs/PLAN.md Task 26/FND-07) has no Step that
// requires a terminal "permanently failed" transition — kernel.WithExternalOp
// treats every fn failure as retryable and leaves the operation reserved
// (see its doc comment), so nothing in this package currently writes
// StateFailed. The constant exists because the migration's CHECK
// constraint already allows the value; a future task can add an explicit
// MarkFailed once a caller needs to distinguish "will retry" from "gave up".
type State string

const (
	StateReserved   State = "reserved"
	StateExecuted   State = "executed"
	StateReconciled State = "reconciled"
	StateFailed     State = "failed"
)

// Op is one external_operations row.
type Op struct {
	ID             OpID
	WorkflowID     string
	Kind           string
	Target         string
	IdempotencyKey string
	State          State
	Request        json.RawMessage
	Receipt        json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ErrOpNotFound is returned when an OpID does not name an existing row.
var ErrOpNotFound = errors.New("extops: operation not found")

// ErrNotReserved is returned by MarkExecuted when the operation is not
// currently in the reserved state — Constitution C9's state machine
// forbids marking an operation executed that was never reserved (or was
// already executed/reconciled/failed).
var ErrNotReserved = errors.New("extops: operation is not in the reserved state")

// ErrNotExecuted is returned by Reconcile when the operation is not
// currently in the executed state — reconciliation only ever applies to
// an operation whose side effect is already recorded as having happened.
var ErrNotExecuted = errors.New("extops: operation is not in the executed state")

// Store is the Postgres-backed external-operation ledger
// (internal/db/migrations/00006_ledgers.sql's external_operations table).
type Store struct {
	db *sql.DB
}

// NewStore wraps an existing *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// newOpID returns a random 128-bit operation id, hex-encoded — same
// scheme as internal/kernel/lease.go's fencing tokens.
func newOpID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("extops: generate op id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Reserve persists the intent to perform an external side effect,
// keyed by idempotencyKey. A second Reserve call for the same
// idempotencyKey is a no-op that returns the existing operation
// (unchanged) rather than an error or a duplicate row — this is the
// unique-key upsert the task card requires.
func (s *Store) Reserve(ctx context.Context, workflowID, kind, target, idempotencyKey string, request any) (Op, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return Op{}, fmt.Errorf("extops: marshal request: %w", err)
	}

	id, err := newOpID()
	if err != nil {
		return Op{}, err
	}

	const insert = `
INSERT INTO external_operations (id, workflow_id, kind, target, idempotency_key, state, request)
VALUES ($1, $2, $3, $4, $5, 'reserved', $6)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id, workflow_id, kind, target, idempotency_key, state, request, receipt, created_at, updated_at`

	op, err := scanOp(s.db.QueryRowContext(ctx, insert, id, workflowID, kind, target, idempotencyKey, payload))
	if err == nil {
		return op, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Op{}, fmt.Errorf("extops: reserve %s: %w", idempotencyKey, err)
	}

	// ON CONFLICT DO NOTHING means another Reserve already won this key —
	// return that existing row, still with no error and no new row.
	op, err = s.getByKey(ctx, idempotencyKey)
	if err != nil {
		return Op{}, fmt.Errorf("extops: reserve %s: load existing: %w", idempotencyKey, err)
	}
	return op, nil
}

// Get loads one operation by id.
func (s *Store) Get(ctx context.Context, id OpID) (Op, error) {
	const q = `
SELECT id, workflow_id, kind, target, idempotency_key, state, request, receipt, created_at, updated_at
FROM external_operations WHERE id = $1`
	op, err := scanOp(s.db.QueryRowContext(ctx, q, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Op{}, ErrOpNotFound
	}
	if err != nil {
		return Op{}, fmt.Errorf("extops: get %s: %w", id, err)
	}
	return op, nil
}

func (s *Store) getByKey(ctx context.Context, idempotencyKey string) (Op, error) {
	const q = `
SELECT id, workflow_id, kind, target, idempotency_key, state, request, receipt, created_at, updated_at
FROM external_operations WHERE idempotency_key = $1`
	return scanOp(s.db.QueryRowContext(ctx, q, idempotencyKey))
}

// MarkExecuted records that opID's side effect has happened, attaching
// receipt as the durable proof. Only a currently-reserved operation may
// be marked executed — an op that was never reserved (unknown id) or one
// already executed/reconciled/failed returns ErrOpNotFound/ErrNotReserved
// rather than silently overwriting a prior receipt.
func (s *Store) MarkExecuted(ctx context.Context, id OpID, receipt any) (Op, error) {
	payload, err := json.Marshal(receipt)
	if err != nil {
		return Op{}, fmt.Errorf("extops: marshal receipt: %w", err)
	}

	const update = `
UPDATE external_operations
SET state = 'executed', receipt = $2, updated_at = now()
WHERE id = $1 AND state = 'reserved'
RETURNING id, workflow_id, kind, target, idempotency_key, state, request, receipt, created_at, updated_at`

	op, err := scanOp(s.db.QueryRowContext(ctx, update, string(id), payload))
	if err == nil {
		return op, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Op{}, fmt.Errorf("extops: mark executed %s: %w", id, err)
	}

	// Zero rows updated: distinguish "doesn't exist" from "wrong state" so
	// callers get an actionable error.
	if _, getErr := s.Get(ctx, id); errors.Is(getErr, ErrOpNotFound) {
		return Op{}, ErrOpNotFound
	}
	return Op{}, ErrNotReserved
}

// Reconcile compares observed against the operation's stored receipt and
// transitions it to reconciled. Only a currently-executed operation may
// be reconciled. diverged reports whether observed differs from the
// receipt recorded at execution time — the caller (internal/ledger's
// Reconciler) is responsible for surfacing that as the
// external_operation_divergence metric.
func (s *Store) Reconcile(ctx context.Context, id OpID, observed json.RawMessage) (diverged bool, err error) {
	const update = `
UPDATE external_operations
SET state = 'reconciled', updated_at = now()
WHERE id = $1 AND state = 'executed'
RETURNING id, workflow_id, kind, target, idempotency_key, state, request, receipt, created_at, updated_at`

	op, err := scanOp(s.db.QueryRowContext(ctx, update, string(id)))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("extops: reconcile %s: %w", id, err)
		}
		if _, getErr := s.Get(ctx, id); errors.Is(getErr, ErrOpNotFound) {
			return false, ErrOpNotFound
		}
		return false, ErrNotExecuted
	}

	return !jsonEqual(op.Receipt, observed), nil
}

// ListByState returns up to limit operations currently in state, ordered
// oldest-first — the reconciler's read path.
func (s *Store) ListByState(ctx context.Context, state State, limit int) ([]Op, error) {
	const q = `
SELECT id, workflow_id, kind, target, idempotency_key, state, request, receipt, created_at, updated_at
FROM external_operations WHERE state = $1 ORDER BY created_at ASC LIMIT $2`

	rows, err := s.db.QueryContext(ctx, q, string(state), limit)
	if err != nil {
		return nil, fmt.Errorf("extops: list by state %s: %w", state, err)
	}
	defer func() { _ = rows.Close() }()

	var ops []Op
	for rows.Next() {
		op, err := scanOpRow(rows)
		if err != nil {
			return nil, fmt.Errorf("extops: scan %s: %w", state, err)
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("extops: list by state %s: %w", state, err)
	}
	return ops, nil
}

// rowScanner is the common subset of *sql.Row and *sql.Rows used by
// scanOp/scanOpRow.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanOp(row rowScanner) (Op, error) {
	return scanOpRow(row)
}

func scanOpRow(row rowScanner) (Op, error) {
	var (
		op      Op
		id      string
		state   string
		request []byte
		receipt []byte
	)
	if err := row.Scan(&id, &op.WorkflowID, &op.Kind, &op.Target, &op.IdempotencyKey, &state, &request, &receipt, &op.CreatedAt, &op.UpdatedAt); err != nil {
		return Op{}, err
	}
	op.ID = OpID(id)
	op.State = State(state)
	op.Request = request
	op.Receipt = receipt
	return op, nil
}

// jsonEqual reports whether a and b encode the same JSON value,
// independent of key order or byte-for-byte formatting. Two empty/nil
// inputs are considered equal.
func jsonEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
