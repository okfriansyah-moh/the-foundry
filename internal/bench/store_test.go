package bench_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// requireGitRefParents skips when mergeRef or its two parents are absent —
// typical of GitHub Actions' default shallow checkout (fetch-depth: 1).
func requireGitRefParents(t *testing.T, repoRoot, mergeRef string) {
	t.Helper()
	for _, ref := range []string{mergeRef, mergeRef + "^1", mergeRef + "^2"} {
		cmd := exec.Command("git", "rev-parse", "--verify", ref)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git history for %s unavailable (shallow clone?): %v (%s)", ref, err, strings.TrimSpace(string(out)))
		}
	}
}

// initMergeRepo builds a tiny merge commit history for hermetic MineDelivery tests.
func initMergeRepo(t *testing.T) (repoRoot, mergeSHA string) {
	t.Helper()
	repoRoot = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=bench",
			"GIT_AUTHOR_EMAIL=bench@example.invalid",
			"GIT_COMMITTER_NAME=bench",
			"GIT_COMMITTER_EMAIL=bench@example.invalid",
			"GIT_AUTHOR_DATE=2026-07-01T10:00:00Z",
			"GIT_COMMITTER_DATE=2026-07-01T10:00:00Z",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-b", "main")
	run("config", "user.email", "bench@example.invalid")
	run("config", "user.name", "bench")
	if err := os.WriteFile(filepath.Join(repoRoot, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "base")
	run("checkout", "-b", "feature")
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "feature work")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=bench",
		"GIT_AUTHOR_EMAIL=bench@example.invalid",
		"GIT_COMMITTER_NAME=bench",
		"GIT_COMMITTER_EMAIL=bench@example.invalid",
		"GIT_AUTHOR_DATE=2026-07-01T11:00:00Z",
		"GIT_COMMITTER_DATE=2026-07-01T11:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("feature commit: %v (%s)", err, out)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "b.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "b.txt")
	cmd = exec.Command("git", "commit", "-m", "add feature file")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=bench",
		"GIT_AUTHOR_EMAIL=bench@example.invalid",
		"GIT_COMMITTER_NAME=bench",
		"GIT_COMMITTER_EMAIL=bench@example.invalid",
		"GIT_AUTHOR_DATE=2026-07-01T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-07-01T12:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("feature file commit: %v (%s)", err, out)
	}
	run("checkout", "main")
	cmd = exec.Command("git", "merge", "--no-ff", "-m", "Merge feature", "feature")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=bench",
		"GIT_AUTHOR_EMAIL=bench@example.invalid",
		"GIT_COMMITTER_NAME=bench",
		"GIT_COMMITTER_EMAIL=bench@example.invalid",
		"GIT_AUTHOR_DATE=2026-07-01T13:00:00Z",
		"GIT_COMMITTER_DATE=2026-07-01T13:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("merge: %v (%s)", err, out)
	}
	mergeSHA = run("rev-parse", "HEAD")
	return repoRoot, mergeSHA
}

func TestMineDelivery_SyntheticRepo(t *testing.T) {
	repoRoot, mergeSHA := initMergeRepo(t)
	g, defects, note, err := bench.MineDelivery(repoRoot, mergeSHA)
	if err != nil {
		t.Fatalf("MineDelivery: %v", err)
	}
	if g.FilesChanged < 1 {
		t.Fatalf("files changed = %d", g.FilesChanged)
	}
	if g.MergedAt.Before(g.FirstCommitAt) {
		t.Fatalf("merged %v before first %v", g.MergedAt, g.FirstCommitAt)
	}
	if defects < 0 {
		t.Fatalf("defects = %v note=%s", defects, note)
	}
	lead := g.MergedAt.Sub(g.FirstCommitAt)
	if lead < time.Hour {
		t.Fatalf("lead time %v too small for fixture", lead)
	}
}

func TestMineDelivery_RealRepo(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	mergeRef := "a57783a98f6fe26257fd9ecb00047581e1204586"
	requireGitRefParents(t, repoRoot, mergeRef)
	g, defects, note, err := bench.MineDelivery(repoRoot, mergeRef)
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
	repoRoot, mergeSHA := initMergeRepo(t)
	dir := t.TempDir()
	manifest := strings.ReplaceAll(`version: "baseline-v1"
deliveries:
  - id: control-synthetic-1
    merge_ref: MERGE
    title: "synthetic delivery 1"
  - id: control-synthetic-2
    merge_ref: MERGE
    title: "synthetic delivery 2"
  - id: control-synthetic-3
    merge_ref: MERGE
    title: "synthetic delivery 3"
human_input:
  control-synthetic-1:
    orchestration_hours: 2
    manual_prompts_touches: 5
    reporter: test
    reported_at: "2026-08-01"
  control-synthetic-2:
    orchestration_hours: 3
    manual_prompts_touches: 6
    reporter: test
    reported_at: "2026-08-01"
  control-synthetic-3:
    orchestration_hours: 4
    manual_prompts_touches: 7
    reporter: test
    reported_at: "2026-08-01"
`, "MERGE", mergeSHA)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// EnvironmentDigest reads go.mod + config/benchmark-targets.yaml relative to repoRoot.
	// Copy those inputs into the synthetic repo so the test stays hermetic.
	ws, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"go.mod", "config/benchmark-targets.yaml"} {
		src := filepath.Join(ws, rel)
		dst := filepath.Join(repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := bench.CopyFile(src, dst); err != nil {
			t.Fatal(err)
		}
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

func TestCaptureBaseline_RealRepo(t *testing.T) {
	dir := t.TempDir()
	manifestSrc := "../../benchmarks/baseline/manifest.yaml"
	if err := bench.CopyFile(manifestSrc, filepath.Join(dir, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	requireGitRefParents(t, repoRoot, "a57783a98f6fe26257fd9ecb00047581e1204586")
	records, err := bench.CaptureBaseline(context.Background(), repoRoot, dir)
	if err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("got %d records", len(records))
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
	// Down rolls back one migration at a time; 00034+ may sit above 00033,
	// so keep rolling until benchmark_runs is gone (proves 00033 Down works).
	for i := 0; i < 32; i++ {
		ok, err = bench.TableExists(ctx, sqlDB)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if err := migrator.Down(ctx); err != nil {
			t.Fatalf("down step %d: %v", i+1, err)
		}
	}
	ok, err = bench.TableExists(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("benchmark_runs still present after rolling past 00033")
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
