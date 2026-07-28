package db

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestNewMigrator_EmbeddedSourcesParse verifies the embedded migration set
// is well-formed goose SQL (correct +goose Up/Down annotations, sequential
// versions) without requiring a live Postgres connection: sql.Open with
// the pgx stdlib driver does not dial until first use, so construction
// alone exercises goose's file parsing.
func TestNewMigrator_EmbeddedSourcesParse(t *testing.T) {
	sqlDB, err := sql.Open("pgx", "postgres://unused/unused")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	m, err := NewMigrator(sqlDB)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}

	sources := m.provider.ListSources()
	wantVersions := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	if len(sources) != len(wantVersions) {
		t.Fatalf("got %d migration sources, want %d", len(sources), len(wantVersions))
	}
	for i, src := range sources {
		if src.Version != wantVersions[i] {
			t.Errorf("source %d: version = %d, want %d (path %s)", i, src.Version, wantVersions[i], src.Path)
		}
	}
}
