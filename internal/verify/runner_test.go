package verify

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func TestRunnerAllPassingCommands(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [go]\n")
	runner := NewRunner(al)

	records, err := runner.Run(context.Background(), ws, []string{"go version"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", records[0].ExitCode)
	}
	if records[0].PolicyViolation || records[0].TimedOut {
		t.Errorf("unexpected classification flags on record: %+v", records[0])
	}
}

func TestRunnerStopsAtFirstAllowlistViolation(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [go]\n")
	runner := NewRunner(al)

	records, err := runner.Run(context.Background(), ws, []string{"go version", "curl http://evil", "go version"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (stop at the violation)", len(records))
	}
	if !records[1].PolicyViolation {
		t.Errorf("record[1] = %+v, want PolicyViolation=true", records[1])
	}
	ok, class := Evaluate(records, 1)
	if ok || class != ClassificationPolicyViolation {
		t.Errorf("Evaluate() = (%v, %q), want (false, %q)", ok, class, ClassificationPolicyViolation)
	}
}

func TestRunnerStopsAtFirstNonzeroExit(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [go]\n")
	runner := NewRunner(al)

	records, err := runner.Run(context.Background(), ws, []string{"go this-subcommand-does-not-exist", "go version"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1 (stop after the failure, never reach the second command)", len(records))
	}
	if records[0].ExitCode == 0 {
		t.Fatalf("expected nonzero exit for an unknown go subcommand, got 0")
	}
}

func TestRunnerTimeout(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [sleep]\n")
	runner := Runner{Allowlist: al, Timeout: 50 * time.Millisecond}

	start := time.Now()
	records, err := runner.Run(context.Background(), ws, []string{"sleep 5"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Run took %s, want it killed well under the 5s sleep", elapsed)
	}
	if len(records) != 1 || !records[0].TimedOut {
		t.Fatalf("records = %+v, want exactly one TimedOut record", records)
	}

	okFirst, classFirst := Evaluate(records, 1)
	if okFirst || classFirst != ClassificationRetryable {
		t.Errorf("Evaluate(attempt=1) = (%v, %q), want (false, %q)", okFirst, classFirst, ClassificationRetryable)
	}
	okSecond, classSecond := Evaluate(records, 2)
	if okSecond || classSecond != ClassificationNoProgress {
		t.Errorf("Evaluate(attempt=2) = (%v, %q), want (false, %q)", okSecond, classSecond, ClassificationNoProgress)
	}
}

// TestLyingExecutorSummaryOverriddenByRealCommandRecords is the
// honest-completion test docs/PLAN.md Task 13 Step 3 requires: an
// executor's self-reported Summary can claim full success, but the task's
// result must be derived solely from Runner's CommandRecords. Evaluate's
// signature does not even accept a Summary, which is the structural
// guarantee that no code path can let one silently override a real exit
// code.
func TestLyingExecutorSummaryOverriddenByRealCommandRecords(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [go]\n")
	runner := NewRunner(al)

	lyingSummary := executor.Summary{Claimed: "all tests pass", ExitNotes: "0 failures"}

	records, err := runner.Run(context.Background(), ws, []string{"go this-subcommand-does-not-exist"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	ok, class := Evaluate(records, 1)
	if ok {
		t.Fatalf("task marked ok despite a real nonzero exit code, even though the executor claimed %q", lyingSummary.Claimed)
	}
	if class != ClassificationVerificationFailed {
		t.Fatalf("classification = %q, want %q", class, ClassificationVerificationFailed)
	}
}

// TestRunnerShellMetacharactersAreInert is the injection corpus the task
// card demands: "; rm -rf", backticks, and $(...) env-expansion must all
// be inert because commands are exec'd argv-style with no shell, never
// interpreted by one.
func TestRunnerShellMetacharactersAreInert(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [go]\n")
	runner := NewRunner(al)

	corpus := []string{
		"go version; touch pwned-semicolon",
		"go version `touch pwned-backtick`",
		"go version $(touch pwned-dollar)",
		"go version && touch pwned-and",
		"go version | touch pwned-pipe",
	}

	for _, cmd := range corpus {
		records, err := runner.Run(context.Background(), ws, []string{cmd})
		if err != nil {
			t.Fatalf("Run(%q): %v", cmd, err)
		}
		if len(records) != 1 {
			t.Fatalf("Run(%q): got %d records, want 1", cmd, len(records))
		}
		if records[0].Cmd != cmd {
			t.Errorf("Cmd = %q, want %q (verbatim, unexpanded)", records[0].Cmd, cmd)
		}
	}

	entries, err := os.ReadDir(ws.Path)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pwned") {
			t.Fatalf("a shell metacharacter was interpreted: found injected file %s", e.Name())
		}
	}
}

// TestRunnerSemicolonPrefixedCommandIsPolicyViolation covers the ";
// rm -rf" shape where the injection is the entire command line, not a
// trailing clause on an allowed one: strings.Fields makes ";rm" (or "; rm"
// with a space, tokenizing to ";" as argv[0]) the first token, which is
// never in any allowlist, so it is refused before exec, not run and
// ignored.
func TestRunnerSemicolonPrefixedCommandIsPolicyViolation(t *testing.T) {
	ws := worktree.Workspace{Path: t.TempDir()}
	al := writeAllowlist(t, "commands: [go]\n")
	runner := NewRunner(al)

	records, err := runner.Run(context.Background(), ws, []string{"; rm -rf /"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(records) != 1 || !records[0].PolicyViolation {
		t.Fatalf("records = %+v, want a single PolicyViolation record", records)
	}
}
