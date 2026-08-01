package kernel_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/stretchr/testify/mock"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// twoIndependentPlanTemplate is a plan whose two tasks declare no dependency on
// each other, so pec.ProposeWaves places them in a SINGLE wave — the case Task
// 124 executes concurrently.
const twoIndependentPlanTemplate = `---
id: plan-kernel-concurrency-fixture
title: Kernel concurrency fixture plan
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/kernel-fixture
    branch: main
tasks:
  - id: t1
    goal: %[1]s
    commands:
      - noop
    validation_commands:
      - go version
    files:
      - a.txt
  - id: t2
    goal: %[1]s
    commands:
      - noop
    validation_commands:
      - go version
    files:
      - b.txt
declared_effects:
  - kind: docs
    target: README.md
requested_permissions:
  - kind: repo-write
    target: "*"
budget_usd: 1.0
---
## Rationale

Fixture for internal/kernel concurrent-wave tests.
`

type concurrencyFixture struct {
	PlanID       string
	PlanFilePath string
	RepoPath     string
	Activities   *kernel.Activities
}

func newConcurrencyFixture(t *testing.T) concurrencyFixture {
	t.Helper()
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "fake_script.yaml")
	if err := os.WriteFile(scriptPath, []byte(scriptSuccess), 0o644); err != nil {
		t.Fatalf("write fake script: %v", err)
	}
	planSource := fmt.Sprintf(twoIndependentPlanTemplate, scriptPath)
	doc, err := plan.ParseBytes([]byte(planSource))
	if err != nil {
		t.Fatalf("parse fixture plan: %v", err)
	}
	allowPath := filepath.Join(dir, "allowlist.yaml")
	if err := os.WriteFile(allowPath, []byte(allowListSource), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	allow, err := provenance.LoadAllowList(allowPath)
	if err != nil {
		t.Fatalf("load allowlist: %v", err)
	}
	decision, err := admission.Classify(doc, admission.NoopPolicyView{})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	now := time.Now().UTC()
	approved, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:              doc.ID,
		PlanDigest:          doc.DigestHex(),
		CreatorPrincipal:    "alice",
		SubmittingPrincipal: "alice",
		ClassifierVersion:   decision.ClassifierVersion,
		Declared:            decision.Declared,
		Requested:           doc.RequestedPermissions,
		Scope:               provenance.Scope{Repositories: []string{"https://github.com/example/kernel-fixture"}},
		RiskTier:            decision.Tier,
		BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: doc.BudgetUSD, WorkflowUSD: doc.BudgetUSD},
		DataClass:           "internal",
		Approvers:           []provenance.Approver{{Principal: "alice", Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:          now,
		ExpiresAt:           now.Add(24 * time.Hour),
	}, allow)
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}
	if err := provenance.Sign(kp.Private, approved); err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	if err := store.Insert(context.Background(), approved); err != nil {
		t.Fatalf("insert approved plan: %v", err)
	}
	planFilePath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planFilePath, []byte(planSource), 0o644); err != nil {
		t.Fatalf("write plan file: %v", err)
	}
	repoPath := initFixtureRepo(t, dir)
	validationAllowPath := filepath.Join(dir, "validation-allowlist.yaml")
	if err := os.WriteFile(validationAllowPath, []byte(validationAllowlistSource), 0o644); err != nil {
		t.Fatalf("write validation allowlist: %v", err)
	}
	validationAllow, err := verify.LoadAllowlist(validationAllowPath)
	if err != nil {
		t.Fatalf("load validation allowlist: %v", err)
	}
	acts := kernel.NewActivities(
		store,
		&worktree.Manager{Root: filepath.Join(dir, "worktrees")},
		evidence.NewFSStore(filepath.Join(dir, "evidence")),
		kernel.NewMemLeaseStore(),
		kernel.NewMemReceiptStore(),
		kernel.NewMemTransitionStore(),
		kernel.NewMemBudgetStore(),
		cost.Defaults{DefaultUSD: 0.10},
		verify.NewRunner(validationAllow),
	)
	wireTestSelection(acts, "fake")
	return concurrencyFixture{PlanID: doc.ID, PlanFilePath: planFilePath, RepoPath: repoPath, Activities: acts}
}

// TestDeliverPlan_ConcurrentWaveOverlap proves docs/PLAN.md Task 124: two
// independent tasks in one wave execute with provably OVERLAPPING execution
// windows (both are in ExecuteTask simultaneously), on distinct worktrees, and
// the workflow succeeds. ExecuteTask is mocked to block on a barrier until both
// tasks have entered it — reachable only if the workflow dispatched them
// concurrently; under sequential dispatch the second would never enter while
// the first is blocked, so max-in-flight would stay 1.
func TestDeliverPlan_ConcurrentWaveOverlap(t *testing.T) {
	fx := newConcurrencyFixture(t)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	var mu sync.Mutex
	inFlight, maxInFlight, entered := 0, 0, 0
	barrier := make(chan struct{})
	worktrees := map[string]string{}
	env.OnActivity(kernel.ActivityExecuteTask, mock.Anything, mock.Anything).Return(
		func(_ context.Context, in kernel.ExecuteTaskInput) (kernel.ExecuteTaskOutput, error) {
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			worktrees[in.TaskID] = in.WorkspacePath
			entered++
			if entered == 2 {
				close(barrier) // both tasks are in-flight together
			}
			mu.Unlock()
			select {
			case <-barrier:
			case <-time.After(3 * time.Second): // guard: serial dispatch never reaches 2
			}
			mu.Lock()
			inFlight--
			mu.Unlock()
			return kernel.ExecuteTaskOutput{ExecutorUsed: "fake"}, nil
		})

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:             fx.PlanID,
		PlanFilePath:       fx.PlanFilePath,
		RepoPath:           fx.RepoPath,
		ExecutorName:       "fake",
		ExecutorAllowlist:  []string{"fake"},
		MaxWaveConcurrency: 2,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result kernel.DeliverPlanResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get result: %v", err)
	}
	if result.Status != string(state.StatusSucceeded) {
		t.Fatalf("status = %q, want SUCCEEDED", result.Status)
	}
	if maxInFlight != 2 {
		t.Fatalf("max concurrent ExecuteTask = %d, want 2 (overlapping execution windows)", maxInFlight)
	}
	// Distinct worktrees per concurrent task (C8 isolation on the real path).
	if worktrees["t1"] == "" || worktrees["t2"] == "" || worktrees["t1"] == worktrees["t2"] {
		t.Fatalf("expected distinct non-empty worktrees, got t1=%q t2=%q", worktrees["t1"], worktrees["t2"])
	}
}

// TestPecWaves_GroupingAndFallback proves the wave grouping and PEC distrust
// fallback (docs/PLAN.md Task 124/56): independent tasks share a wave, a
// dependency chain serializes into one-task waves, and a cyclic proposal falls
// back to a per-task sequential plan.
func TestPecWaves_GroupingAndFallback(t *testing.T) {
	independent := []plan.Task{{ID: "t1"}, {ID: "t2"}}
	waves := kernel.PecWavesForTest(independent)
	if len(waves) != 1 || len(waves[0]) != 2 {
		t.Fatalf("independent tasks: waves = %v, want a single 2-task wave", waves)
	}

	chain := []plan.Task{{ID: "t1"}, {ID: "t2", DependsOn: []string{"t1"}}, {ID: "t3", DependsOn: []string{"t2"}}}
	waves = kernel.PecWavesForTest(chain)
	if len(waves) != 3 {
		t.Fatalf("dependency chain: waves = %v, want 3 sequential waves", waves)
	}

	cyclic := []plan.Task{{ID: "t1", DependsOn: []string{"t2"}}, {ID: "t2", DependsOn: []string{"t1"}}}
	waves = kernel.PecWavesForTest(cyclic)
	if len(waves) != 2 {
		t.Fatalf("cyclic proposal must fall back to per-task sequential waves, got %v", waves)
	}
	for _, w := range waves {
		if len(w) != 1 {
			t.Fatalf("sequential fallback wave must hold exactly one task, got %v", w)
		}
	}
}
