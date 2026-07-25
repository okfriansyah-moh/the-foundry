package projection

import (
	"context"
	"database/sql"
	"fmt"
)

// RebuildResult reports the outcome of a Rebuild: the row count and digest
// of the freshly rebuilt projection, for the caller (CLI, e2e test) to
// compare against a value captured before the table was dropped/truncated
// (docs/PLAN.md Task 14 Acceptance: "drop table -> rebuild -> identical
// checksum").
type RebuildResult struct {
	Rows     int64
	Checksum string
}

// Rebuild truncates workflow_status_projection and resets this projector's
// offset, then replays every transition from seq 0 through the same Tick
// logic the live projector loop uses (docs/PLAN.md Task 14 Step 3). It
// asserts internal completeness — the rebuilt row count must equal the
// number of distinct workflows in workflow_transitions — before returning
// the checksum computed by the projection_checksum() SQL function
// (internal/db/migrations/00003_projection.sql).
func Rebuild(ctx context.Context, db *sql.DB, name string) (RebuildResult, error) {
	var result RebuildResult

	if _, err := db.ExecContext(ctx, `TRUNCATE workflow_status_projection`); err != nil {
		return result, fmt.Errorf("projection: truncate projection: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projection_offsets WHERE projector = $1`, name); err != nil {
		return result, fmt.Errorf("projection: reset offset: %w", err)
	}

	p := &Projector{DB: db, Name: name}
	for {
		n, err := p.Tick(ctx)
		if err != nil {
			return result, fmt.Errorf("projection: replay tick: %w", err)
		}
		if n == 0 {
			break
		}
	}

	var expectedRows int64
	if err := db.QueryRowContext(ctx, `SELECT count(DISTINCT workflow_id) FROM workflow_transitions`).Scan(&expectedRows); err != nil {
		return result, fmt.Errorf("projection: count distinct workflows: %w", err)
	}

	var actualRows int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workflow_status_projection`).Scan(&actualRows); err != nil {
		return result, fmt.Errorf("projection: count projected rows: %w", err)
	}
	if actualRows != expectedRows {
		return result, fmt.Errorf("projection: rebuild row-count mismatch: got %d projected rows, want %d distinct workflows", actualRows, expectedRows)
	}

	var checksum string
	if err := db.QueryRowContext(ctx, `SELECT projection_checksum()`).Scan(&checksum); err != nil {
		return result, fmt.Errorf("projection: compute checksum: %w", err)
	}

	result.Rows = actualRows
	result.Checksum = checksum
	return result, nil
}
