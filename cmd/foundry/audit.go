package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	auditpkg "github.com/okfriansyah-moh/the-foundry/internal/audit"
)

const auditVerifyTimeout = 30 * time.Second

// runAuditVerify implements `foundry audit verify` (docs/PLAN.md Task 39 /
// FND-20 M1-exit Acceptance: "audit chain verify — writer from migration
// 0008 + `foundry audit verify`"). It re-derives the audit_log hash chain
// from Postgres and reports whether every row is internally consistent and
// correctly linked to its predecessor (provenance.VerifyAuditChain) — a
// read-only check with no side effects.
func runAuditVerify(args []string) error {
	fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
	pgDSN := fs.String("pg-dsn", "", "Postgres DSN (defaults to $PG_DSN)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("audit verify: usage: foundry audit verify [--pg-dsn DSN]")
	}

	dsn := *pgDSN
	if dsn == "" {
		dsn = pgDSNFromEnv()
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("audit verify: open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), auditVerifyTimeout)
	defer cancel()

	rows, err := auditpkg.LoadRows(ctx, db)
	if err != nil {
		return fmt.Errorf("audit verify: %w", err)
	}
	result := auditpkg.VerifyRows(rows)
	if !result.OK {
		if result.BadSeq != 0 {
			return fmt.Errorf("audit verify: FAIL — audit_log row seq=%d does not match its own recomputed hash (payload or hash tampered)", result.BadSeq)
		}
		return fmt.Errorf("audit verify: FAIL — audit_log row seq=%d does not chain to its predecessor's stored hash (row deleted, reordered, or inserted out of band)", result.BrokenLinkSeq)
	}
	anchors := auditpkg.BuildAnchors(rows, 10000, time.Now().UTC())
	fmt.Printf("PASS: audit_log hash chain verified (%d rows, %d anchors)\n", result.RowCount, len(anchors))
	return nil
}
