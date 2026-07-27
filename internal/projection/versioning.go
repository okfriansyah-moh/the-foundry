// versioning.go implements docs/PLAN.md Task 38 (FND-19)'s versioned
// projector rollout: build the next projector version into a shadow
// table via full replay from seq 0, prove that backfill is reproducible
// (checksum-compare across two independent passes, bounded retries so
// concurrently running workflows never make this fail spuriously), then
// atomically rename shadow -> live. See Rollout's doc comment for why the
// comparison is shadow-against-itself rather than shadow-against-live.
//
// Governing doc: docs/foundry/docs/architecture/data-consistency.md §2 —
// "projector schema migrations: deploy new projector version alongside,
// backfill, cut over, then retire — never in-place mutation of live
// projection semantics." ShadowTable is the "alongside" deployment
// target; the rename swap below is the "cut over"; PreviousTable is the
// one-generation "retire" (dropped by the next rollout, not immediately,
// so an operator can inspect/restore it if the swap surfaces a problem).
package projection

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	// ShadowTable is the fixed shadow-projection table Rollout backfills
	// into (internal/db/migrations/00011_projection_versioning.sql). One
	// shadow generation in flight at a time — data-consistency.md §2
	// describes a single alongside/backfill/cutover sequence, not N
	// concurrent in-progress rollouts.
	ShadowTable = "workflow_status_projection_shadow"

	// PreviousTable retains exactly one prior generation after a swap.
	// The next Rollout call drops it before reusing the name.
	PreviousTable = "workflow_status_projection_previous"

	// ShadowProjectorName is the shadow projector's own projection_offsets
	// key — distinct from DefaultProjectorName so a rollout in progress
	// never touches the live projector's offset.
	ShadowProjectorName = "workflow_status_projection_shadow"

	// maxConvergeAttempts bounds the checksum-compare retry loop (the
	// card's "checksum-compare window"): under concurrent writes the
	// shadow backfill chases a moving target, so Rollout re-drains and
	// re-compares up to this many times before giving up rather than ever
	// swapping an unverified or diverged shadow table into place.
	maxConvergeAttempts = 10
)

// RolloutResult reports what Rollout did, for the CLI and e2e test to
// display/assert on.
type RolloutResult struct {
	FromVersion      string
	ToVersion        string
	Rows             int64
	Checksum         string
	ConvergeAttempts int
}

// Rollout performs the full versioned-projector cutover described above.
//
// Zero-update-loss argument (docs/PLAN.md Task 38 Acceptance: "a live
// rollout during running workflows loses zero updates"): the shadow
// backfill and the atomic swap only ever touch workflow_status_projection
// / workflow_status_projection_shadow — never workflow_transitions, the
// append-only source of truth kernel activities write to (Constitution
// C4). No transition is ever read from or written to during the swap
// itself, so nothing the kernel appends can be lost by this operation.
// Because projection_offsets tracks a projector's progress against
// workflow_transitions.seq — never against a specific physical table —
// any Tick call using DefaultProjectorName continues correctly against
// whichever physical table is currently named workflow_status_projection
// after a swap, with no reset and no reprocessing: every transition that
// arrived during the rollout (whether the shadow backfill saw it or not)
// is picked up by the next Tick against the now-live (ex-shadow) table.
//
// Checksum-compare window (the card's phrase): Rollout does not compare
// the shadow table against the live table directly — a workflow that
// receives a new transition mid-rollout legitimately advances past any
// fixed watermark in the shadow's re-derivation while the live table
// (never touched by this function) still reflects its old state, which
// would make a live-vs-shadow comparison fail for a reason that has
// nothing to do with the new version's correctness. Instead Rollout
// proves the *new* version's backfill is reproducible, the same
// "drop table -> rebuild -> identical checksum" contract Task 14's
// Rebuild() already established (internal/projection/rebuild.go):
// truncate-and-replay the shadow table twice in a row; if the total
// transition count and the resulting content digest are identical across
// both passes, nothing arrived between them and the derivation is stable
// enough to cut over. If workflows keep producing transitions faster than
// two consecutive full backfills can complete without anything new
// arriving in between, Rollout retries up to maxConvergeAttempts times
// before giving up rather than ever swapping an unverified table.
//
// Known interaction with Rebuild: rebuild.go's Rebuild always stamps rows
// with the package ProjectorVersion constant (a plain default Projector,
// not Rollout's target version). Calling `foundry projection rebuild`
// after a successful Rollout re-derives correct *content* but reverts
// every row's projector_version label back to ProjectorVersion. The
// package constant is expected to be bumped to match once a rolled-out
// version is the accepted new default — Rollout intentionally does not
// mutate that constant itself, since a rollout only proves the shadow
// table's content; promoting it as this package's own default is a
// separate, deliberate code change.
func Rollout(ctx context.Context, db *sql.DB, toVersion string) (RolloutResult, error) {
	var result RolloutResult
	result.FromVersion = ProjectorVersion
	result.ToVersion = toVersion

	if toVersion == "" {
		return result, fmt.Errorf("projection: rollout: toVersion is required")
	}

	shadow := &Projector{DB: db, Name: ShadowProjectorName, Table: ShadowTable, Version: toVersion}

	converged := false
	var checksum string
	for attempt := 1; attempt <= maxConvergeAttempts; attempt++ {
		result.ConvergeAttempts = attempt

		if err := resetShadow(ctx, db); err != nil {
			return result, err
		}
		if err := drainProjector(ctx, shadow); err != nil {
			return result, fmt.Errorf("projection: backfill shadow pass A (attempt %d): %w", attempt, err)
		}
		countA, err := transitionCount(ctx, db)
		if err != nil {
			return result, err
		}
		var checksumA string
		if err := db.QueryRowContext(ctx, `SELECT projection_checksum_shadow()`).Scan(&checksumA); err != nil {
			return result, fmt.Errorf("projection: checksum shadow (pass A): %w", err)
		}

		if err := resetShadow(ctx, db); err != nil {
			return result, err
		}
		if err := drainProjector(ctx, shadow); err != nil {
			return result, fmt.Errorf("projection: backfill shadow pass B (attempt %d): %w", attempt, err)
		}
		countB, err := transitionCount(ctx, db)
		if err != nil {
			return result, err
		}
		var checksumB string
		if err := db.QueryRowContext(ctx, `SELECT projection_checksum_shadow()`).Scan(&checksumB); err != nil {
			return result, fmt.Errorf("projection: checksum shadow (pass B): %w", err)
		}

		if countA == countB && checksumA == checksumB {
			converged = true
			checksum = checksumB
			break
		}
	}
	if !converged {
		return result, fmt.Errorf("projection: rollout did not converge to a reproducible backfill after %d attempts (live workflows kept producing transitions between checksum passes) — retry", maxConvergeAttempts)
	}
	result.Checksum = checksum

	if err := swapTables(ctx, db, &result); err != nil {
		return result, err
	}
	return result, nil
}

// resetShadow truncates the shadow table and clears its offset, so the
// next drainProjector call replays from seq 0.
func resetShadow(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "TRUNCATE "+ShadowTable); err != nil {
		return fmt.Errorf("projection: truncate shadow table: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projection_offsets WHERE projector = $1`, ShadowProjectorName); err != nil {
		return fmt.Errorf("projection: reset shadow offset: %w", err)
	}
	return nil
}

// transitionCount is the total row count of workflow_transitions at this
// instant — used to detect whether anything new arrived between two
// backfill passes.
func transitionCount(ctx context.Context, db *sql.DB) (int64, error) {
	var n int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workflow_transitions`).Scan(&n); err != nil {
		return 0, fmt.Errorf("projection: count transitions: %w", err)
	}
	return n, nil
}

// swapTables performs the atomic cut-over: retire the current live table,
// promote the converged shadow table in its place, and recreate an empty
// shadow table so the next Rollout call has somewhere to backfill into.
// All four DDL statements run inside one transaction — Postgres DDL is
// transactional, so a failure partway through leaves the live table
// exactly as it was (data-consistency.md §2: "never in-place mutation of
// live projection semantics" until the swap is proven complete).
//
// Every identifier below (DefaultTable, PreviousTable, ShadowTable) is a
// fixed Go string constant defined in this package — never built from
// request/user input — so this string concatenation carries no SQL
// injection surface (OWASP A05); Postgres DDL statements cannot bind
// identifiers as query parameters, so this is the standard safe pattern
// for a closed, compile-time-known set of table names.
func swapTables(ctx context.Context, db *sql.DB, result *RolloutResult) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("projection: begin swap tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []string{
		"DROP TABLE IF EXISTS " + PreviousTable,
		"ALTER TABLE " + DefaultTable + " RENAME TO " + PreviousTable,
		"ALTER TABLE " + ShadowTable + " RENAME TO " + DefaultTable,
		"CREATE TABLE " + ShadowTable + " (LIKE " + DefaultTable + " INCLUDING ALL)",
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("projection: atomic swap step %q: %w", stmt, err)
		}
	}

	var rows int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+DefaultTable).Scan(&rows); err != nil {
		return fmt.Errorf("projection: count rows post-swap: %w", err)
	}
	result.Rows = rows

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("projection: commit swap: %w", err)
	}
	return nil
}

// drainProjector Ticks p until a batch returns zero processed rows — i.e.
// fully caught up to whatever is currently in workflow_transitions.
func drainProjector(ctx context.Context, p *Projector) error {
	for {
		n, err := p.Tick(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	}
}
