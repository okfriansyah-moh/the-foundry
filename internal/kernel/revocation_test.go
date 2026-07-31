package kernel_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

const threeTaskPlanTemplate = `---
id: plan-kernel-revocation-fixture
title: Kernel revocation fixture plan
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
      - README.md
  - id: t2
    goal: %[1]s
    commands:
      - noop
    validation_commands:
      - go version
    files:
      - README.md
  - id: t3
    goal: %[1]s
    commands:
      - noop
    validation_commands:
      - go version
    files:
      - README.md
declared_effects:
  - kind: docs
    target: README.md
requested_permissions:
  - kind: repo-write
    target: "*"
budget_usd: 1.0
---
## Rationale

Fixture for internal/kernel mid-flight revocation tests.
`

// revocationFixture is like kernelFixture but for a three-task plan, and it
// also exposes the Store and KeyPair a test needs to revoke the
// ApprovedPlan out-of-band while the workflow is mid-flight.
type revocationFixture struct {
	PlanID       string
	PlanFilePath string
	RepoPath     string
	Activities   *kernel.Activities
	Transitions  *kernel.MemTransitionStore
	Store        *provenance.Store
	KeyPair      *provenance.KeyPair
}

func newRevocationFixture(t *testing.T) revocationFixture {
	t.Helper()
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "fake_script.yaml")
	if err := os.WriteFile(scriptPath, []byte(scriptSuccess), 0o644); err != nil {
		t.Fatalf("write fake script: %v", err)
	}

	planSource := fmt.Sprintf(threeTaskPlanTemplate, scriptPath)
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

	transitions := kernel.NewMemTransitionStore()
	acts := kernel.NewActivities(
		store,
		&worktree.Manager{Root: filepath.Join(dir, "worktrees")},
		evidence.NewFSStore(filepath.Join(dir, "evidence")),
		kernel.NewMemLeaseStore(),
		kernel.NewMemReceiptStore(),
		transitions,
		kernel.NewMemBudgetStore(),
		cost.Defaults{DefaultUSD: 0.10},
		verify.NewRunner(validationAllow),
	)
	wireTestSelection(acts, "fake")

	return revocationFixture{
		PlanID:       doc.ID,
		PlanFilePath: planFilePath,
		RepoPath:     repoPath,
		Activities:   acts,
		Transitions:  transitions,
		Store:        store,
		KeyPair:      kp,
	}
}

// TestDeliverPlan_MidFlightRevocation is Task 24's crux acceptance case
// (docs/PLAN.md Task 24 Acceptance): "revoking during task 2 of a 3-task
// plan halts before task 3 with correct terminal + audit entries." The
// ApprovedPlan is revoked out-of-band (simulating an operator running
// `foundry plan revoke` concurrently) the instant task 2's ExecuteTask
// activity starts — by the time the workflow's task loop reaches task 3,
// RecheckApproval must catch the revocation and the workflow must end
// FAILED/ADMISSION_REJECTED without ever starting task 3.
func TestDeliverPlan_MidFlightRevocation(t *testing.T) {
	fx := newRevocationFixture(t)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	revoked := false
	executeTaskCount := 0
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, args converter.EncodedValues) {
		if info.ActivityType.Name != kernel.ActivityExecuteTask {
			return
		}
		executeTaskCount++
		if revoked {
			return
		}
		var in kernel.ExecuteTaskInput
		if err := args.Get(&in); err != nil {
			t.Fatalf("decode ExecuteTaskInput: %v", err)
		}
		if in.TaskID != "t2" {
			return
		}
		revoked = true
		if _, err := fx.Store.Revoke(context.Background(), fx.PlanID, fx.KeyPair.Private, "security-team", "mid-flight test revocation"); err != nil {
			t.Fatalf("revoke mid-flight: %v", err)
		}
	})

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:            fx.PlanID,
		PlanFilePath:      fx.PlanFilePath,
		RepoPath:          fx.RepoPath,
		ExecutorName:      "fake",
		ExecutorAllowlist: []string{"fake"},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !revoked {
		t.Fatal("test setup bug: revocation callback never fired — task2 ExecuteTask was never observed")
	}

	var result kernel.DeliverPlanResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusFailed) {
		t.Fatalf("status = %q, want %q", result.Status, state.StatusFailed)
	}
	if result.ResultCode != "ADMISSION_REJECTED" {
		t.Fatalf("result code = %q, want ADMISSION_REJECTED", result.ResultCode)
	}

	// t1 and t2 both ran ExecuteTask (t2 succeeded before the revocation
	// took effect); t3's ExecuteTask must never have started — its
	// RecheckApproval activity fails before AcquireLease/AcquireWorktree/
	// ExecuteTask are ever reached, which is exactly why runTask still
	// records a (failed) TaskResult for it without ever executing it.
	if executeTaskCount != 2 {
		t.Fatalf("ExecuteTask ran %d times, want exactly 2 (t1, t2) — t3 must never execute after mid-flight revocation", executeTaskCount)
	}
	if len(result.Tasks) != 3 {
		t.Fatalf("tasks = %+v, want 3 entries (t1 ok, t2 ok, t3 failed-without-running)", result.Tasks)
	}
	if result.Tasks[0].TaskID != "t1" || result.Tasks[0].Failed {
		t.Fatalf("tasks[0] = %+v, want t1 succeeded", result.Tasks[0])
	}
	if result.Tasks[1].TaskID != "t2" || result.Tasks[1].Failed {
		t.Fatalf("tasks[1] = %+v, want t2 succeeded", result.Tasks[1])
	}
	if result.Tasks[2].TaskID != "t3" || !result.Tasks[2].Failed {
		t.Fatalf("tasks[2] = %+v, want t3 failed (rejected by RecheckApproval, never executed)", result.Tasks[2])
	}

	// No orphaned worktrees: every worktree this run created (one per
	// completed task) must have been released.
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(fx.PlanFilePath), "worktrees"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read worktrees root: %v", err)
	}
	for _, e := range entries {
		if e.Name() == ".meta" || e.Name() == ".locks" {
			continue
		}
		wfDir := filepath.Join(filepath.Dir(fx.PlanFilePath), "worktrees", e.Name())
		taskDirs, err := os.ReadDir(wfDir)
		if err != nil {
			t.Fatalf("read workflow worktree dir: %v", err)
		}
		if len(taskDirs) != 0 {
			t.Fatalf("expected no orphaned worktrees under %s, found: %v", wfDir, taskDirs)
		}
	}

	workflowID := "default-test-workflow-id"
	transitions := fx.Transitions.All(workflowID)
	if len(transitions) != 2 {
		t.Fatalf("transitions = %d, want 2 (RUNNING then FAILED)", len(transitions))
	}
	if transitions[1].Status != state.StatusFailed {
		t.Fatalf("transitions[1].Status = %q, want FAILED", transitions[1].Status)
	}
}
