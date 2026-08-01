package bench_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/bench"
	"github.com/okfriansyah-moh/the-foundry/internal/db"
)

func testDSN() string {
	if v := os.Getenv("BENCH_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

func TestMineDelivery_RealRepo(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	g, defects, note, err := bench.MineDelivery(repoRoot, "a57783a98f6fe26257fd9ecb00047581e1204586")
	if err != nil {
		t.Fatalf("MineDelivery: %v", err)
	}
	if g.FilesChanged < 50 {
		t.Fatalf("files changed = %d, want substantial PR", g.FilesChanged)
	}
	if g.MergedAt.Before(g.FirstCommitAt) {
		t.Fatal("merged before first commit")
	}
	if defects < 0 {
		t.Fatalf("defects = %v note=%s", defects, note)
	}
}

func TestCaptureBaseline_WritesRecords(t *testing.T) {
	dir := t.TempDir()
	manifestSrc := "../../benchmarks/baseline/manifest.yaml"
	if err := bench.CopyFile(manifestSrc, filepath.Join(dir, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	records, err := bench.CaptureBaseline(context.Background(), repoRoot, dir)
	if err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("got %d records", len(records))
	}
	store := bench.NewFileStore(dir)
	ids, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) < 3 {
		t.Fatalf("list ids = %v", ids)
	}
}

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := bench.NewFileStore(dir)
	rec := bench.NewRunRecord("test-1", bench.ArmControl, "wi", "title", "digest")
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("test-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rec.ID || got.Arm != bench.ArmControl {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestPostgresStore_SaveLoad(t *testing.T) {
	dsn := testDSN()
	if dsn == "" {
		t.Skip("BENCH_TEST_PG_DSN/PG_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	migrator, err := db.NewMigrator(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := bench.NewPostgresStore(sqlDB)
	rec := bench.NewRunRecord("bench-pg-test", bench.ArmControl, "wi", "title", "digest")
	t.Cleanup(func() { _ = store.Delete(ctx, rec.ID) })
	if err := store.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rec.ID {
		t.Fatalf("got id %s", got.ID)
	}
}

func TestMigration00033_Down(t *testing.T) {
	dsn := testDSN()
	if dsn == "" {
		t.Skip("BENCH_TEST_PG_DSN/PG_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	migrator, err := db.NewMigrator(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	ok, err := bench.TableExists(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("benchmark_runs missing after up")
	}
	if err := migrator.Down(ctx); err != nil {
		t.Fatalf("down: %v", err)
	}
	ok, err = bench.TableExists(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("benchmark_runs still present after down")
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("re-up: %v", err)
	}
}

func TestEnvironmentDigest(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	d1, err := bench.EnvironmentDigest(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := bench.EnvironmentDigest(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == "" || d1 != d2 {
		t.Fatalf("digest unstable: %q vs %q", d1, d2)
	}
}
