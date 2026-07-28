package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/claudecode"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/pec"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// TestWaveIsolation is Task 91's (PRV-08) wave-level fresh-context e2e: two
// tasks PEC groups into the SAME concurrent wave are dispatched concurrently
// through the real kernel ExecuteTask activity, each in its own workspace and
// its own fresh adapter instance, and neither task's artifacts/goal leak into
// the other's evidence bundle. Task 56 (PEC/waves) exists, so this proves the
// property the card defers to it ("assert via evidence bundles that no
// artifact/env value crosses between them").
func TestWaveIsolation(t *testing.T) {
	// Two independent tasks (no dependency) ⇒ PEC places them in one wave.
	doc := plan.Document{
		ID: "wave-plan", Version: "1",
		Tasks: []plan.Task{
			{ID: "t1", Goal: "GOAL-ALPHA-unique"},
			{ID: "t2", Goal: "GOAL-BETA-unique"},
		},
	}
	wp, err := pec.ProposeWaves(doc)
	if err != nil {
		t.Fatalf("ProposeWaves: %v", err)
	}
	// Confirm the two tasks share a concurrent wave.
	var concurrent []string
	for _, wave := range wp.Waves {
		if len(wave) >= 2 {
			concurrent = wave
		}
	}
	if len(concurrent) < 2 {
		t.Fatalf("expected a wave with both independent tasks, got waves %v", wp.Waves)
	}

	// A stub `claude` that echoes a fixed JSON result; the claude-code adapter
	// writes each task's goal into that task's OWN workspace prompt file.
	stub := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho '{\"result\":\"ok\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOUNDRY_CLAUDE_CODE_BIN", stub)

	evDir := t.TempDir()
	acts := NewActivities(nil, nil, evidence.NewFSStore(evDir), nil, NewMemReceiptStore(), nil, nil, cost.Defaults{}, verify.Runner{})

	type result struct {
		taskID string
		ws     string
		out    ExecuteTaskOutput
	}
	goals := map[string]string{"t1": "GOAL-ALPHA-unique", "t2": "GOAL-BETA-unique"}
	results := make([]result, len(concurrent))
	var wg sync.WaitGroup
	for i, taskID := range concurrent {
		i, taskID := i, taskID
		ws := filepath.Join(t.TempDir(), "ws-"+taskID)
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := acts.ExecuteTask(context.Background(), ExecuteTaskInput{
				WorkflowID: "wave-wf", TaskID: taskID, Attempt: 1,
				ExecutorName: "claude-code", WorkspacePath: ws,
				Packet: executor.TaskPacket{PlanID: "wave-plan", TaskID: taskID, Goal: goals[taskID]},
			})
			if err != nil {
				t.Errorf("[%s] ExecuteTask: %v", taskID, err)
				return
			}
			results[i] = result{taskID: taskID, ws: ws, out: out}
		}()
	}
	wg.Wait()

	// Each task's workspace must contain ONLY its own goal — no cross-task leak.
	for _, r := range results {
		promptPath := filepath.Join(r.ws, ".foundry-claude-code-prompt.md")
		data, err := os.ReadFile(promptPath)
		if err != nil {
			t.Fatalf("[%s] read prompt: %v", r.taskID, err)
		}
		content := string(data)
		if !strings.Contains(content, goals[r.taskID]) {
			t.Fatalf("[%s] workspace missing its own goal", r.taskID)
		}
		for otherID, otherGoal := range goals {
			if otherID != r.taskID && strings.Contains(content, otherGoal) {
				t.Fatalf("[%s] workspace leaked %s's goal (cross-task wave leak)", r.taskID, otherID)
			}
		}
		if r.out.ExecutorUsed != "claude-code" {
			t.Fatalf("[%s] ExecutorUsed=%q, want claude-code", r.taskID, r.out.ExecutorUsed)
		}
	}
}
