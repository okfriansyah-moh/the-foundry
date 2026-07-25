package fake

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func init() {
	executor.Register("fake", func() executor.Adapter { return &Fake{} })
}

// Fake is a deterministic executor.Adapter driven by a Script loaded from
// testdata. The path to that script is carried in TaskPacket.Goal — the
// fake executor has no real goal to interpret, so this field is repurposed
// to reference the packet's testdata (decision: smallest reversible choice,
// since TaskPacket has no dedicated script-path field per the Task 10 card).
type Fake struct {
	script Script
	wsPath string
}

// New constructs a fresh Fake adapter.
func New() *Fake { return &Fake{} }

// Prepare loads the script referenced by packet.Goal.
func (f *Fake) Prepare(_ context.Context, ws worktree.Workspace, packet executor.TaskPacket) error {
	if packet.Goal == "" {
		return fmt.Errorf("fake: packet.Goal must reference a fake_script.yaml path")
	}
	script, err := LoadScript(packet.Goal)
	if err != nil {
		return fmt.Errorf("fake: prepare: %w", err)
	}
	f.script = script
	f.wsPath = ws.Path
	return nil
}

// Run applies the script's patches and returns its (untrusted) Summary. It
// returns an error when the script's exit_code is nonzero, mirroring a real
// executor whose commands failed — even when, as with the "lie" fixture,
// Claimed says otherwise.
func (f *Fake) Run(ctx context.Context) (executor.Summary, error) {
	if f.script.SleepMS > 0 {
		select {
		case <-time.After(time.Duration(f.script.SleepMS) * time.Millisecond):
		case <-ctx.Done():
			return executor.Summary{}, fmt.Errorf("fake: run: %w", ctx.Err())
		}
	}

	for _, p := range f.script.Patches {
		full := filepath.Join(f.wsPath, p.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return executor.Summary{}, fmt.Errorf("fake: apply patch %s: %w", p.Path, err)
		}
		if err := os.WriteFile(full, []byte(p.Content), 0o644); err != nil {
			return executor.Summary{}, fmt.Errorf("fake: apply patch %s: %w", p.Path, err)
		}
	}

	summary := executor.Summary{Claimed: f.script.Claimed, ExitNotes: f.script.ExitNotes}
	if f.script.ExitCode != 0 {
		return summary, fmt.Errorf("fake: script exited %d", f.script.ExitCode)
	}
	return summary, nil
}

// Collect returns the paths written by Run's patches.
func (f *Fake) Collect(context.Context) (executor.Artifacts, error) {
	paths := make([]string, 0, len(f.script.Patches))
	for _, p := range f.script.Patches {
		paths = append(paths, p.Path)
	}
	return executor.Artifacts{Paths: paths}, nil
}
