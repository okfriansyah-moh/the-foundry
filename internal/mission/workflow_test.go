package mission

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// --- test fakes: interfaces defined in workflow.go, in-memory here so
// go test ./internal/mission/... never needs a live Postgres. ---

type fakeLoopContracts struct{ registered bool }

func (f *fakeLoopContracts) HasLoopContract(_ context.Context, _ string) (bool, error) {
	return f.registered, nil
}

type fakeMissionState struct {
	mu   sync.Mutex
	rows []StateSnapshot
}

type fakeReadiness struct{ pass bool }

func (f *fakeReadiness) HasPassingReadinessArtifact(_ context.Context, _ string) (bool, error) {
	return f.pass, nil
}

func (f *fakeMissionState) RecordState(_ context.Context, snap StateSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, snap)
	return nil
}

func (f *fakeMissionState) all() []StateSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]StateSnapshot, len(f.rows))
	copy(out, f.rows)
	return out
}

type fakeGateEvents struct {
	mu    sync.Mutex
	count int
}

func (f *fakeGateEvents) RecordGateEvent(_ context.Context, _, _ string, _ time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	return fmt.Sprintf("gate-%d", f.count), nil
}

func (f *fakeGateEvents) ResolveGateEvent(_ context.Context, _ string, _ string, _ time.Time) error {
	return nil
}

// fakeBudgetStore returns a healthy provisioned envelope by default (Task 119:
// a mission has an envelope). setExhausted makes a kind exhausted;
// setNoEnvelope makes a kind report cost.ErrBudgetNotFound so the fail-closed
// no-envelope path can be exercised.
type fakeBudgetStore struct {
	mu         sync.Mutex
	exhausted  map[cost.Kind]bool
	noEnvelope map[cost.Kind]bool
}

func newFakeBudgetStore() *fakeBudgetStore {
	return &fakeBudgetStore{exhausted: map[cost.Kind]bool{}, noEnvelope: map[cost.Kind]bool{}}
}

func (f *fakeBudgetStore) setExhausted(kind cost.Kind, v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exhausted[kind] = v
}

func (f *fakeBudgetStore) setNoEnvelope(kind cost.Kind, v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.noEnvelope[kind] = v
}

func (f *fakeBudgetStore) GetBudget(_ context.Context, _ cost.Scope, _ string, kind cost.Kind, _ string) (cost.Budget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noEnvelope[kind] {
		return cost.Budget{}, cost.ErrBudgetNotFound
	}
	if f.exhausted[kind] {
		return cost.Budget{CeilingUSD: 100, IncurredUSD: 100}, nil // ceiling - incurred == 0: exhausted
	}
	return cost.Budget{CeilingUSD: 100, IncurredUSD: 0}, nil // healthy provisioned envelope
}

// fakeNetMRRSource returns a fixed, pre-built queue of samples in order,
// one per call, regardless of the `at` argument -- tests control the
// simulated observation time via each sample's own At field so the
// evaluator's confirmation-window math is exercised deterministically
// without needing to align it with the Temporal test environment's mocked
// clock.
type fakeNetMRRSource struct {
	mu      sync.Mutex
	samples []LedgerSample
	calls   int
}

func (f *fakeNetMRRSource) Observe(_ context.Context, _ string, _ time.Time) (LedgerSample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls >= len(f.samples) {
		// Ran out of scripted samples: report unavailable rather than
		// panicking, so a test that kills the mission early doesn't need
		// to size its sample queue exactly.
		f.calls++
		return LedgerSample{Available: false}, nil
	}
	s := f.samples[f.calls]
	f.calls++
	return s, nil
}

// missionFixture bundles a MissionLoop-ready Activities set plus its
// underlying fakes, so each test only configures what it cares about.
type missionFixture struct {
	Activities    *Activities
	LoopContracts *fakeLoopContracts
	Readiness     *fakeReadiness
	MissionState  *fakeMissionState
	GateEvents    *fakeGateEvents
	Budgets       *fakeBudgetStore
	NetMRR        *fakeNetMRRSource
	Transitions   *kernel.MemTransitionStore
}

func newMissionFixture(samples []LedgerSample) *missionFixture {
	lc := &fakeLoopContracts{registered: true}
	ready := &fakeReadiness{pass: true}
	ms := &fakeMissionState{}
	ge := &fakeGateEvents{}
	budgets := newFakeBudgetStore()
	netmrr := &fakeNetMRRSource{samples: samples}
	transitions := kernel.NewMemTransitionStore()

	return &missionFixture{
		Activities:    NewActivities(lc, ready, ms, ge, ge, transitions, budgets, netmrr),
		LoopContracts: lc,
		Readiness:     ready,
		MissionState:  ms,
		GateEvents:    ge,
		Budgets:       budgets,
		NetMRR:        netmrr,
		Transitions:   transitions,
	}
}

func registerMissionActivities(env *testsuite.TestWorkflowEnvironment, a *Activities) {
	env.RegisterActivityWithOptions(a.RequireLoopContract, activity.RegisterOptions{Name: ActivityRequireLoopContract})
	env.RegisterActivityWithOptions(a.RequireReadiness, activity.RegisterOptions{Name: ActivityRequireReadiness})
	env.RegisterActivityWithOptions(a.ObserveLedger, activity.RegisterOptions{Name: ActivityObserveLedger})
	env.RegisterActivityWithOptions(a.CheckBudget, activity.RegisterOptions{Name: ActivityCheckBudget})
	env.RegisterActivityWithOptions(a.AppendMissionTransition, activity.RegisterOptions{Name: ActivityAppendMissionTransition})
	env.RegisterActivityWithOptions(a.RecordMissionState, activity.RegisterOptions{Name: ActivityRecordMissionState})
	env.RegisterActivityWithOptions(a.RecordGateEvent, activity.RegisterOptions{Name: ActivityRecordGateEvent})
	env.RegisterActivityWithOptions(a.ResolveGateEvent, activity.RegisterOptions{Name: ActivityResolveGateEvent})
}

// meetingSampleAt is meetingSample (evaluator_test.go) with an explicit day
// offset, reused here to build a scripted sample queue.
func meetingSampleAt(offsetDays int) LedgerSample { return meetingSample(offsetDays) }

func TestMissionLoop_RefusesWithoutLoopContract(t *testing.T) {
	fx := newMissionFixture(nil)
	fx.LoopContracts.registered = false

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("want a workflow error (refuses to start without a loop contract), got nil")
	}
}

func TestMissionLoop_CeremonyReadinessRequired(t *testing.T) {
	fx := newMissionFixture(nil)
	fx.Readiness.pass = false

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("want a workflow error (missing readiness pass), got nil")
	}
}

func TestMissionLoop_SustainedTargetSucceeds(t *testing.T) {
	samples := make([]LedgerSample, 0, 32)
	for day := 0; day <= 31; day++ {
		samples = append(samples, meetingSampleAt(day))
	}
	fx := newMissionFixture(samples)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MissionLoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusSucceeded) {
		t.Fatalf("Status = %q, want SUCCEEDED", result.Status)
	}
	if result.ResultCode != string(state.ResultMissionTargetReached) {
		t.Fatalf("ResultCode = %q, want %q", result.ResultCode, state.ResultMissionTargetReached)
	}

	rows := fx.MissionState.all()
	if len(rows) == 0 {
		t.Fatal("want mission_state rows recorded")
	}
	if rows[len(rows)-1].Status != string(state.StatusSucceeded) {
		t.Fatalf("last mission_state row status = %q, want SUCCEEDED", rows[len(rows)-1].Status)
	}
}

// TestMissionLoop_KillMidLoop proves docs/PLAN.md Task 40's Acceptance:
// killing a mission while it is actively observing ends the workflow
// CANCELLED/MISSION_KILLED with a non-empty handoff note.
func TestMissionLoop_KillMidLoop(t *testing.T) {
	fx := newMissionFixture(nil) // never reaches an observe cycle before the kill signal lands

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalKillMission, KillRequest{RequestedBy: "alice", Reason: "pivoting away from this product"})
	}, time.Millisecond)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MissionLoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusCancelled) {
		t.Fatalf("Status = %q, want CANCELLED", result.Status)
	}
	if result.ResultCode != string(state.ResultMissionKilled) {
		t.Fatalf("ResultCode = %q, want %q", result.ResultCode, state.ResultMissionKilled)
	}
	if result.HandoffNote == "" {
		t.Fatal("HandoffNote is empty, want a clean product-state handoff note")
	}
}

// TestMissionLoop_KillWhilePaused proves killing a mission that is
// currently WAITING (here: on an exhausted monthly budget) also ends
// CANCELLED/MISSION_KILLED, exercising the wait-selector's kill branch
// distinct from the main observing loop's.
func TestMissionLoop_KillWhilePaused(t *testing.T) {
	samples := []LedgerSample{meetingSampleAt(0)}
	fx := newMissionFixture(samples)
	fx.Budgets.setExhausted(cost.KindMissionMonthly, true)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalKillMission, KillRequest{RequestedBy: "bob", Reason: "budget will not be raised"})
	}, 25*time.Hour) // after the first daily observe tick has paused it

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MissionLoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusCancelled) || result.ResultCode != string(state.ResultMissionKilled) {
		t.Fatalf("result = %+v, want CANCELLED/MISSION_KILLED", result)
	}

	rows := fx.MissionState.all()
	foundWaiting := false
	for _, r := range rows {
		if r.Status == string(state.StatusWaiting) && r.Reason == string(state.ReasonBudget) {
			foundWaiting = true
		}
	}
	if !foundWaiting {
		t.Fatal("want a recorded WAITING/budget mission_state row before the kill")
	}
}

// TestMissionLoop_ManualPauseThenResume proves `foundry mission pause`'s
// signal (SignalManualPause) pauses WAITING/human-command independent of
// any evaluator-driven pause_when trigger, and that SignalResumeMission
// resumes it back to RUNNING (mirroring the evaluator-driven pause path's
// own resume behavior via the shared pauseAndWait helper).
func TestMissionLoop_ManualPauseThenResume(t *testing.T) {
	fx := newMissionFixture(nil)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalManualPause, PauseRequest{RequestedBy: "alice", Reason: "planned maintenance"})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalResumeMission, nil)
	}, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalKillMission, KillRequest{RequestedBy: "alice", Reason: "test complete"})
	}, 3*time.Millisecond)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MissionLoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusCancelled) {
		t.Fatalf("Status = %q, want CANCELLED", result.Status)
	}

	transitions := fx.Transitions.All("default-test-workflow-id")
	sawPause, sawResume := false, false
	for i, tr := range transitions {
		if tr.Status == state.StatusWaiting && tr.Reason == state.ReasonHumanCommand {
			sawPause = true
		}
		if sawPause && tr.Status == state.StatusRunning && i > 0 && transitions[i-1].Status == state.StatusWaiting {
			sawResume = true
		}
	}
	if !sawPause {
		t.Fatal("want a WAITING/human-command transition from the manual pause")
	}
	if !sawResume {
		t.Fatal("want a WAITING->RUNNING transition after the resume signal")
	}
}

func TestMissionLoop_UnforeseenGateRoundTrip(t *testing.T) {
	fx := newMissionFixture(nil)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalEnterHumanGate, HumanGateRequest{
			RequestedBy: "ops",
			Action:      "accept-invite-to-payment-provider",
		})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalResumeMission, nil)
	}, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalKillMission, KillRequest{RequestedBy: "ops", Reason: "test done"})
	}, 3*time.Millisecond)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if fx.GateEvents.count == 0 {
		t.Fatal("want a recorded gate event for unforeseen human action")
	}
	transitions := fx.Transitions.All("default-test-workflow-id")
	sawWait := false
	sawResume := false
	for i, tr := range transitions {
		if tr.Status == state.StatusWaiting && tr.Reason == state.ReasonUnforeseenHumanGate {
			sawWait = true
		}
		if sawWait && i > 0 && transitions[i-1].Status == state.StatusWaiting && tr.Status == state.StatusRunning {
			sawResume = true
		}
	}
	if !sawWait || !sawResume {
		t.Fatalf("unforeseen gate round trip not observed (sawWait=%v sawResume=%v)", sawWait, sawResume)
	}
}

// --- child-workflow ("must call into DeliverPlan, not duplicate it")
// proof: a real kernel.DeliverPlan runs as MissionLoop's child workflow on
// SignalTriggerDelivery, using the same fixture-building blocks
// internal/kernel's own workflow_test.go uses. ---

const deliveryFixturePlanSource = `---
id: plan-mission-fixture
title: Mission fixture plan
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/mission-fixture
    branch: main
tasks:
  - id: t1
    goal: %s
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

Fixture for internal/mission's MissionLoop -> DeliverPlan child workflow test.
`

const deliveryFixtureAllowList = `
permissions:
  - kind: repo-write
    target: "*"
`

const deliveryFixtureScriptSuccess = `
patches:
  - path: out.txt
    content: "done\n"
claimed: "all good"
exit_code: 0
`

// deliveryFixtureValidationAllowlist is the internal/verify.Allowlist
// deliveryFixture's Activities.Validator checks its "go version"
// validation command against (docs/PLAN.md Task 99 / SKP-11R).
const deliveryFixtureValidationAllowlist = `
commands:
  - go
scripts_dir: ./scripts/
`

// deliveryFixture builds a real, signed ApprovedPlan plus kernel.Activities
// the same shape internal/kernel's own tests use, so this test proves
// MissionLoop's SignalTriggerDelivery path genuinely drives the real
// kernel.DeliverPlan workflow rather than a look-alike stand-in.
func deliveryFixture(t *testing.T) (kernel.DeliverPlanInput, *kernel.Activities, *kernel.MemTransitionStore) {
	t.Helper()
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "fake_script.yaml")
	if err := os.WriteFile(scriptPath, []byte(deliveryFixtureScriptSuccess), 0o644); err != nil {
		t.Fatalf("write fake script: %v", err)
	}

	planSource := fmt.Sprintf(deliveryFixturePlanSource, scriptPath)
	doc, err := plan.ParseBytes([]byte(planSource))
	if err != nil {
		t.Fatalf("parse fixture plan: %v", err)
	}

	allowPath := filepath.Join(dir, "allowlist.yaml")
	if err := os.WriteFile(allowPath, []byte(deliveryFixtureAllowList), 0o644); err != nil {
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
		Scope:               provenance.Scope{Repositories: []string{"https://github.com/example/mission-fixture"}},
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

	validationAllowPath := filepath.Join(dir, "validation-allowlist.yaml")
	if err := os.WriteFile(validationAllowPath, []byte(deliveryFixtureValidationAllowlist), 0o644); err != nil {
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
	// Task 116 (SEC-02): ExecuteTask fails closed without an allowlist, so the
	// child DeliverPlan must route through real, policy-checked selection.
	acts.ExecutorSelector = kernel.ExecutorSelector{Default: "fake"}
	acts.CapabilityRegistry = capability.Registry{Executors: []capability.Record{
		{Provider: "fake", ExecutionClass: "test", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
	}}

	return kernel.DeliverPlanInput{
		PlanID:            doc.ID,
		PlanFilePath:      planFilePath,
		RepoPath:          repoPath,
		ExecutorName:      "fake",
		ExecutorAllowlist: []string{"fake"},
	}, acts, transitions
}

func registerKernelActivities(env *testsuite.TestWorkflowEnvironment, a *kernel.Activities) {
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

// TestMissionLoop_TriggersRealDeliverPlanChildWorkflow proves docs/PLAN.md
// Task 40's Scope requirement architecturally: MissionLoop's
// SignalTriggerDelivery path calls into the real internal/kernel.DeliverPlan
// workflow (registered on the same test environment, with its own real
// activities) rather than duplicating any of DeliverPlan's own logic.
func TestMissionLoop_TriggersRealDeliverPlanChildWorkflow(t *testing.T) {
	deliverIn, kernelActs, kernelTransitions := deliveryFixture(t)
	fx := newMissionFixture(nil)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	registerKernelActivities(env, kernelActs)
	env.RegisterWorkflow(MissionLoop)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalTriggerDelivery, deliverIn)
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalKillMission, KillRequest{RequestedBy: "system", Reason: "test complete"})
	}, time.Second)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{MissionID: "m1", Contract: testContract()})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MissionLoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusCancelled) {
		t.Fatalf("Status = %q, want CANCELLED (killed after the delivery child workflow ran)", result.Status)
	}

	transitions := kernelTransitions.All("default-test-workflow-id-delivery-1")
	if len(transitions) == 0 {
		t.Fatal("want the child DeliverPlan workflow to have appended its own transitions, proving it actually ran")
	}
	if transitions[len(transitions)-1].Status != state.StatusSucceeded {
		t.Fatalf("child DeliverPlan final status = %q, want SUCCEEDED", transitions[len(transitions)-1].Status)
	}
}
