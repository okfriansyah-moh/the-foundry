// Command m4harness is the driver for docs/PLAN.md Task 93 (PRV-10), the M4
// milestone-exit e2e. It proves the milestone's exit criteria against the
// REAL kernel executor selector, the REAL capability registry
// (config/executor-capabilities.yaml), and the REAL routing table
// (config/executor-routing.yaml) — no stubs for the selection path itself.
//
// It drives internal/kernel.Activities.ExecuteTask directly (an ordinary Go
// method, no Temporal needed — same technique as test/helpers/execonce),
// with gated-stub provider binaries wired via each adapter's FOUNDRY_*_BIN
// test seam so the adapters run without real API spend. It asserts:
//
//   - 3 tasks each explicitly naming a different executor (claude-code,
//     opencode, gemini-cli) are selected inside internal/kernel and reach
//     executor.Get, each recording its own ExecutorUsed on the evidence bundle;
//   - 1 task naming no executor but a routed class resolves via the routing
//     table (Task 90);
//   - 1 task naming a not-allowlisted executor fails CLOSED with the exact
//     policy-violation classification from Task 85;
//   - PhaseHint (Task 92) is present in a claude-code prompt when set and
//     absent when empty.
//
// Every bundle is written to evidence/m4-exit/ as the milestone's archive.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"

	// Register every shipped executor adapter so executor.Get resolves them.
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/claudecode"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/copilot"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/cursor"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/geminicli"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/opencode"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/windsurf"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("m4harness: FAIL: %v", err)
	}
	fmt.Println("m4harness: PASS — all M4 exit assertions held")
}

// supportedAllowlist is the policy allowlist for the happy-path scenarios.
var supportedAllowlist = []string{"claude-code", "opencode", "gemini-cli", "cursor", "copilot", "windsurf"}

func run() error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return err
	}

	reg, err := capability.Load(filepath.Join(repoRoot, "config", "executor-capabilities.yaml"))
	if err != nil {
		return fmt.Errorf("load capability registry: %w", err)
	}
	routing, err := kernel.LoadRoutingTable(filepath.Join(repoRoot, "config", "executor-routing.yaml"))
	if err != nil {
		return fmt.Errorf("load routing table: %w", err)
	}

	// Gated-stub provider binaries: the adapters run these instead of the
	// real CLIs, so no API spend. claude-code parses JSON; the rest capture
	// raw stdout.
	stubDir, err := os.MkdirTemp("", "m4stubs")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stubDir) }()
	claudeStub := writeStub(stubDir, "claude", `echo '{"result":"ok"}'`)
	rawStub := writeStub(stubDir, "raw", `echo ok`)
	if err := os.Setenv("FOUNDRY_CLAUDE_CODE_BIN", claudeStub); err != nil {
		return fmt.Errorf("setenv FOUNDRY_CLAUDE_CODE_BIN: %w", err)
	}
	for _, kv := range [][2]string{
		{"FOUNDRY_OPENCODE_BIN", rawStub},
		{"FOUNDRY_GEMINI_CLI_BIN", rawStub},
		{"FOUNDRY_CURSOR_BIN", rawStub},
		{"FOUNDRY_COPILOT_BIN", rawStub},
	} {
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return fmt.Errorf("setenv %s: %w", kv[0], err)
		}
	}

	evidenceRoot := filepath.Join(repoRoot, "evidence", "m4-exit")
	if err := os.RemoveAll(evidenceRoot); err != nil {
		return err
	}
	store := evidence.NewFSStore(evidenceRoot)

	activities := kernel.NewActivities(nil, nil, store, nil, kernel.NewMemReceiptStore(), nil, nil, cost.Defaults{}, verify.Runner{})
	activities.CapabilityRegistry = reg
	activities.ExecutorSelector = kernel.ExecutorSelector{Default: "claude-code", Routing: routing, Profile: "personal"}

	type scenario struct {
		name          string
		taskID        string
		explicit      string
		class         string
		allowlist     []string
		phaseHint     string
		wantExecutor  string
		wantFailed    bool
		wantClass     string
		wantPhaseIn   bool // assert the phase-hint section is present in the (claude) prompt
		checkPhaseOut bool // assert the phase-hint section is ABSENT
	}
	scenarios := []scenario{
		{name: "explicit-claude-code", taskID: "t1", explicit: "claude-code", allowlist: supportedAllowlist, phaseHint: "I", wantExecutor: "claude-code", wantPhaseIn: true},
		{name: "explicit-opencode", taskID: "t2", explicit: "opencode", allowlist: supportedAllowlist, phaseHint: "I", wantExecutor: "opencode"},
		{name: "explicit-gemini-cli", taskID: "t3", explicit: "gemini-cli", allowlist: supportedAllowlist, phaseHint: "I", wantExecutor: "gemini-cli"},
		{name: "routed-backend-class", taskID: "t4", class: "backend", allowlist: supportedAllowlist, phaseHint: "I", wantExecutor: "opencode"},
		{name: "denied-not-allowlisted", taskID: "t5", explicit: "windsurf", allowlist: []string{"claude-code"}, wantExecutor: "windsurf", wantFailed: true, wantClass: "policy-violation"},
		{name: "claude-code-no-hint", taskID: "t6", explicit: "claude-code", allowlist: supportedAllowlist, phaseHint: "", wantExecutor: "claude-code", checkPhaseOut: true},
	}

	for _, sc := range scenarios {
		ws := filepath.Join(stubDir, "ws-"+sc.taskID)
		if err := os.MkdirAll(ws, 0o755); err != nil {
			return err
		}
		out, err := activities.ExecuteTask(context.Background(), kernel.ExecuteTaskInput{
			WorkflowID:        "m4-exit",
			TaskID:            sc.taskID,
			Attempt:           1,
			ExplicitExecutor:  sc.explicit,
			TaskClass:         sc.class,
			ExecutorAllowlist: sc.allowlist,
			WorkspacePath:     ws,
			Packet: executor.TaskPacket{
				PlanID:    "m4-exit",
				TaskID:    sc.taskID,
				Goal:      "M4 exit fixture task " + sc.taskID,
				PhaseHint: sc.phaseHint,
			},
		})
		if err != nil {
			return fmt.Errorf("[%s] ExecuteTask returned infra error: %w", sc.name, err)
		}
		if out.Failed != sc.wantFailed {
			return fmt.Errorf("[%s] Failed=%v, want %v (msg=%q)", sc.name, out.Failed, sc.wantFailed, out.ErrorMessage)
		}
		if out.ExecutorUsed != sc.wantExecutor {
			return fmt.Errorf("[%s] ExecutorUsed=%q, want %q", sc.name, out.ExecutorUsed, sc.wantExecutor)
		}
		if sc.wantClass != "" && out.Classification != sc.wantClass {
			return fmt.Errorf("[%s] Classification=%q, want %q", sc.name, out.Classification, sc.wantClass)
		}

		// PhaseHint present/absent assertions on the written claude prompt.
		if sc.wantPhaseIn || sc.checkPhaseOut {
			promptPath := filepath.Join(ws, ".foundry-claude-code-prompt.md")
			data, rerr := os.ReadFile(promptPath)
			if rerr != nil {
				return fmt.Errorf("[%s] read claude prompt: %w", sc.name, rerr)
			}
			has := strings.Contains(string(data), "Phase hint")
			if sc.wantPhaseIn && !has {
				return fmt.Errorf("[%s] expected PhaseHint section present in prompt", sc.name)
			}
			if sc.checkPhaseOut && has {
				return fmt.Errorf("[%s] expected PhaseHint section ABSENT in prompt (empty hint)", sc.name)
			}
		}

		// Record the evidence bundle (ExecutorUsed stamped) into the archive.
		ev, err := activities.RecordEvidence(context.Background(), kernel.RecordEvidenceInput{
			WorkflowID:    "m4-exit",
			TaskID:        sc.taskID,
			Attempt:       1,
			WorkspacePath: ws,
			ArtifactPaths: out.ArtifactPaths,
			ExecuteFailed: out.Failed,
			ExecutorUsed:  out.ExecutorUsed,
		})
		if err != nil {
			return fmt.Errorf("[%s] RecordEvidence: %w", sc.name, err)
		}

		// Read the bundle back and confirm ExecutorUsed survived to disk.
		if !sc.wantFailed {
			bundle, err := store.Get(ev.BundleID)
			if err != nil {
				return fmt.Errorf("[%s] read back evidence bundle: %w", sc.name, err)
			}
			if bundle.Manifest.ExecutorUsed != sc.wantExecutor {
				return fmt.Errorf("[%s] persisted ExecutorUsed=%q, want %q", sc.name, bundle.Manifest.ExecutorUsed, sc.wantExecutor)
			}
		}
		fmt.Printf("m4harness: ok [%s] executor=%q failed=%v classification=%q\n", sc.name, out.ExecutorUsed, out.Failed, out.Classification)
	}

	// Staleness of the shipped registry must be clean at exit.
	if stale := reg.Stale(nowUTC()); len(stale) > 0 {
		return fmt.Errorf("capability registry has stale records at M4 exit: %v", stale)
	}

	summary := filepath.Join(evidenceRoot, "SUMMARY.txt")
	if err := os.WriteFile(summary, []byte(exitSummary), 0o644); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

const exitSummary = `M4 (Provider Breadth & Adaptive Execution) exit evidence — docs/PLAN.md Task 93 / PRV-10.

Proven against the real kernel selector, real capability registry, and real routing table:
- 3 explicit-executor tasks (claude-code, opencode, gemini-cli) selected inside internal/kernel, each ExecutorUsed recorded on its bundle.
- 1 routed-default task (class=backend) resolved via config/executor-routing.yaml to opencode.
- 1 denied executor (windsurf, not in allowlist) failed closed with classification policy-violation.
- PhaseHint present when set, absent when empty (claude-code prompt).
- capability-registry staleness lint clean.
`

func writeStub(dir, name, body string) string {
	p := filepath.Join(dir, name)
	_ = os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	return p
}

func nowUTC() time.Time { return time.Now().UTC() }
