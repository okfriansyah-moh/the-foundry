// Package db provides the goose-backed migration wrapper for Task 20
// (FND-01, docs/PLAN.md). It owns no business logic — only schema
// evolution plumbing (Up/Down/Status) so both the CLI (via `make
// migrate-up/migrate-down/migrate-status`) and other packages/tests can
// run migrations programmatically without shelling out to the goose CLI.
//
// Migration files live in ./migrations (embedded below) and are the
// single source of truth for schema: migrations/00001-00003 port the
// original raw-SQL migrations (Tasks 8, 12, 14) into goose format
// unchanged; 00004-00008 add the M1 core schemas (Task 20).
//
// Governing doc: docs/foundry/docs/architecture/data-consistency.md (PG
// authority list) — every table this package's migrations create is
// annotated with a COMMENT ON TABLE stating authoritative vs. rebuildable
// projection (Constitution C3).
package db
