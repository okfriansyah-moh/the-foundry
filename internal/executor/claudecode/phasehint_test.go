package claudecode

import (
	"context"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
)

// TestPhaseHint_EmptyIsByteIdentical proves an empty PhaseHint produces a
// prompt byte-identical to the pre-Task-92 rendering (no behavior change when
// absent), and that a set PhaseHint is purely APPENDED as a labeled section.
func TestPhaseHint_EmptyIsByteIdentical(t *testing.T) {
	base := executor.TaskPacket{
		PlanID:             "p1",
		TaskID:             "t1",
		Goal:               "do the thing",
		Commands:           []string{"go build ./..."},
		ValidationCommands: []string{"go test ./..."},
	}
	withEmpty := renderPrompt(base)
	if strings.Contains(withEmpty, "Phase hint") {
		t.Fatalf("empty PhaseHint must not emit a phase-hint section, got:\n%s", withEmpty)
	}

	hinted := base
	hinted.PhaseHint = "M"
	withHint := renderPrompt(hinted)
	if !strings.HasPrefix(withHint, withEmpty) {
		t.Fatalf("set PhaseHint must only APPEND to the empty-hint prompt (byte-identical prefix)")
	}
	if !strings.Contains(withHint, "phase M") {
		t.Fatalf("set PhaseHint must render the phase letter, got:\n%s", withHint)
	}
	if !strings.Contains(withHint, "non-authoritative") {
		t.Fatalf("phase-hint section must state it is non-authoritative")
	}
}

// TestPhaseHint_NotInArgvNoAuthority proves the hint is never folded into the
// command line and grants no authority: a lying Summary ("done") with a
// "ship"-phase (M) hint is treated identically to one without a hint — a
// nonzero exit is still an error regardless of the claim or the hint.
func TestPhaseHint_NotInArgvNoAuthority(t *testing.T) {
	run := func(hint string) error {
		dir := t.TempDir()
		// Stub claims success but exits nonzero; PhaseHint must not rescue it.
		stub := writeStub(t, dir, "claude", `echo '{"result":"done"}'; exit 3`)
		t.Setenv(binaryEnvOverride, stub)
		ws := newWorkspace(t)
		a := New()
		if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello", PhaseHint: hint}); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		_, err := a.Run(context.Background())
		return err
	}
	errNoHint := run("")
	errShipHint := run("M")
	if errNoHint == nil || errShipHint == nil {
		t.Fatalf("a lying Summary with nonzero exit must error with or without a hint (no-hint=%v ship-hint=%v)", errNoHint, errShipHint)
	}
}
