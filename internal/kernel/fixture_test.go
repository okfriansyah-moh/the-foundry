package kernel_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// planSource is a minimal single-task plan whose task's Goal references a
// fake_script.yaml path (the fake executor's own convention, per
// internal/executor/fake's doc.go) filled in by newFixture at test time.
// validation_commands is also a %[2]s placeholder (docs/PLAN.md Task 99 /
// SKP-11R): "noop" is not on config/validation-allowlist.yaml's real
// allowlist, so callers pick a real, allowlisted command — newFixture's
// own default is "go version"; newFixtureWithValidation lets a test name
// a different one to exercise a genuine validation failure.
const planSourceTemplate = `---
id: plan-kernel-fixture
title: Kernel fixture plan
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
      - %[2]s
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

Fixture for internal/kernel workflow tests.
`

const allowListSource = `
permissions:
  - kind: repo-write
    target: "*"
`

// validationAllowlistSource is the internal/verify.Allowlist this
// fixture's Activities.Validator checks every validation command against
// (docs/PLAN.md Task 13 Step 2) — a minimal stand-in for the real
// config/validation-allowlist.yaml, allowlisting only "go" since every
// fixture's own validation commands are go subcommands.
const validationAllowlistSource = `
commands:
  - go
scripts_dir: ./scripts/
`

// kernelFixture bundles everything DeliverPlan needs: a signed
// ApprovedPlan backed by an in-memory provenance store, a real git repo
// for worktree.Manager to operate on, and a fresh Activities set backed by
// in-memory lease/receipt/transition stores plus a real evidence FSStore.
type kernelFixture struct {
	PlanID       string
	PlanFilePath string
	ScriptPath   string
	RepoPath     string
	Activities   *kernel.Activities
	Transitions  *kernel.MemTransitionStore
	BudgetStore  *kernel.MemBudgetStore
}

// newFixture builds a kernelFixture whose single task runs the given fake
// executor script (exitCode 0 == success, nonzero == failure), validated
// by the real internal/verify.Runner against "go version" (a real,
// allowlisted, always-succeeding command — the default every test not
// specifically exercising validation failure wants).
func newFixture(t *testing.T, scriptYAML string) kernelFixture {
	t.Helper()
	return newFixtureWithValidation(t, scriptYAML, "go version")
}

// newFixtureWithValidation is newFixture with the task's single
// validation command overridden, so a test can exercise a genuine
// validation-command failure/policy-violation independently of whether
// the fake executor itself claims success (docs/PLAN.md Task 99 /
// SKP-11R's honest-completion proof: the executor's claim must never be
// what decides the outcome).
func newFixtureWithValidation(t *testing.T, scriptYAML, validationCmd string) kernelFixture {
	t.Helper()
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "fake_script.yaml")
	if err := os.WriteFile(scriptPath, []byte(scriptYAML), 0o644); err != nil {
		t.Fatalf("write fake script: %v", err)
	}

	planSource := fmt.Sprintf(planSourceTemplate, scriptPath, validationCmd)
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
	budgetStore := kernel.NewMemBudgetStore()
	acts := kernel.NewActivities(
		store,
		&worktree.Manager{Root: filepath.Join(dir, "worktrees")},
		evidence.NewFSStore(filepath.Join(dir, "evidence")),
		kernel.NewMemLeaseStore(),
		kernel.NewMemReceiptStore(),
		transitions,
		budgetStore,
		cost.Defaults{DefaultUSD: 0.10},
		verify.NewRunner(validationAllow),
	)
	// Task 116 (SEC-02): ExecuteTask now fails closed on an absent/empty
	// executor allowlist, so every test that drives a task must route through
	// real, policy-checked selection. Wire the deterministic selector + a
	// capability registry that supports the test executors.
	wireTestSelection(acts, "fake")

	return kernelFixture{
		PlanID:       doc.ID,
		PlanFilePath: planFilePath,
		ScriptPath:   scriptPath,
		RepoPath:     repoPath,
		Activities:   acts,
		Transitions:  transitions,
		BudgetStore:  budgetStore,
	}
}

// initFixtureRepo creates a minimal git repository worktree.Manager.Acquire
// can branch off, under dir/repo.
func initFixtureRepo(t *testing.T, dir string) string {
	t.Helper()
	repoPath := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return repoPath
}

// registerActivities registers every Activities method onto env under the
// name workflow.go's activity constants reference — the same wiring
// cmd/foundryd/main.go does for the real worker.
func registerActivities(env *testsuite.TestWorkflowEnvironment, a *kernel.Activities) {
	env.RegisterActivityWithOptions(a.LoadApprovedPlan, activity.RegisterOptions{Name: kernel.ActivityLoadApprovedPlan})
	env.RegisterActivityWithOptions(a.RecheckApproval, activity.RegisterOptions{Name: kernel.ActivityRecheckApproval})
	env.RegisterActivityWithOptions(a.ReserveBudget, activity.RegisterOptions{Name: kernel.ActivityReserveBudget})
	env.RegisterActivityWithOptions(a.AcquireLease, activity.RegisterOptions{Name: kernel.ActivityAcquireLease})
	env.RegisterActivityWithOptions(a.AcquireWorktree, activity.RegisterOptions{Name: kernel.ActivityAcquireWorktree})
	env.RegisterActivityWithOptions(a.ReleaseWorktree, activity.RegisterOptions{Name: kernel.ActivityReleaseWorktree})
	env.RegisterActivityWithOptions(a.ExecuteTask, activity.RegisterOptions{Name: kernel.ActivityExecuteTask})
	env.RegisterActivityWithOptions(a.ValidateTask, activity.RegisterOptions{Name: kernel.ActivityValidateTask})
	env.RegisterActivityWithOptions(a.RecordEvidence, activity.RegisterOptions{Name: kernel.ActivityRecordEvidence})
	env.RegisterActivityWithOptions(a.AppendTransition, activity.RegisterOptions{Name: kernel.ActivityAppendTransition})
}

// testCapabilityRegistry returns a capability registry that marks the test
// executors ("fake", "claude-code") supported, so ExecutorSelector.Select
// resolves them under a policy allowlist (Task 116 fail-closed selection).
func testCapabilityRegistry() capability.Registry {
	return capability.Registry{Executors: []capability.Record{
		{Provider: "fake", ExecutionClass: "test", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
		{Provider: "claude-code", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
	}}
}

// wireTestSelection configures acts so an allowlisted task routes to
// defaultExecutor through the deterministic selector.
func wireTestSelection(acts *kernel.Activities, defaultExecutor string) {
	acts.ExecutorSelector = kernel.ExecutorSelector{Default: defaultExecutor}
	acts.CapabilityRegistry = testCapabilityRegistry()
}

// fakeAllowlist is the executor allowlist every fixture-driven test task carries
// now that ExecuteTask fails closed without one.
func fakeAllowlist() []string { return []string{"fake"} }
