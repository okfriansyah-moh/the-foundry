package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// heartbeatInterval is how often ExecuteTask reports liveness to Temporal
// while the executor adapter runs (docs/PLAN.md Task 12 Step: "heartbeats
// every 10s").
const heartbeatInterval = 10 * time.Second

// Activities bundles every side-effecting operation DeliverPlan calls out
// to. It is the only place in this package that touches the world —
// workflow.go must never construct or call these directly outside of
// workflow.ExecuteActivity.
type Activities struct {
	ProvenanceStore *provenance.Store
	WorktreeManager *worktree.Manager
	EvidenceStore   evidence.Store
	LeaseStore      LeaseStore
	ReceiptStore    ReceiptStore
	TransitionStore TransitionStore

	mu         sync.Mutex
	workspaces map[string]worktree.Workspace
}

// NewActivities builds an Activities set from its dependencies.
func NewActivities(
	provenanceStore *provenance.Store,
	worktreeManager *worktree.Manager,
	evidenceStore evidence.Store,
	leaseStore LeaseStore,
	receiptStore ReceiptStore,
	transitionStore TransitionStore,
) *Activities {
	return &Activities{
		ProvenanceStore: provenanceStore,
		WorktreeManager: worktreeManager,
		EvidenceStore:   evidenceStore,
		LeaseStore:      leaseStore,
		ReceiptStore:    receiptStore,
		TransitionStore: transitionStore,
		workspaces:      make(map[string]worktree.Workspace),
	}
}

func workspaceKey(workflowID, taskID string) string { return workflowID + "/" + taskID }

func (a *Activities) storeWorkspace(workflowID, taskID string, ws worktree.Workspace) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.workspaces[workspaceKey(workflowID, taskID)] = ws
}

func (a *Activities) loadWorkspace(workflowID, taskID string) (worktree.Workspace, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, ok := a.workspaces[workspaceKey(workflowID, taskID)]
	return ws, ok
}

func (a *Activities) deleteWorkspace(workflowID, taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.workspaces, workspaceKey(workflowID, taskID))
}

// LoadApprovedPlanInput is LoadApprovedPlan's input.
type LoadApprovedPlanInput struct {
	PlanID       string
	PlanFilePath string
}

// LoadApprovedPlanOutput is LoadApprovedPlan's output.
type LoadApprovedPlanOutput struct {
	PlanID   string
	RiskTier string
	Tasks    []plan.Task
}

// LoadApprovedPlan verifies the on-disk plan file at in.PlanFilePath still
// matches its ApprovedPlan's signed digest (internal/provenance,
// Constitution C7) and that its granted permissions are still a subset of
// what was requested, then parses the plan's task list. A tampered file, a
// forged/corrupted stored approval, or a granted-permission escape all
// surface as an error here — deterministic failures that must not retry.
func (a *Activities) LoadApprovedPlan(ctx context.Context, in LoadApprovedPlanInput) (LoadApprovedPlanOutput, error) {
	result, err := provenance.VerifyPlanFile(ctx, a.ProvenanceStore, in.PlanID, in.PlanFilePath)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: load approved plan %s: %w", in.PlanID, err)
	}
	if !result.DigestMatches {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: plan file %s no longer matches its approved digest", in.PlanFilePath)
	}
	if !result.GrantedSubset {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: approved plan %s grants permissions outside requested scope", in.PlanID)
	}

	approved, err := a.ProvenanceStore.Load(ctx, in.PlanID)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: reload approved plan %s: %w", in.PlanID, err)
	}

	raw, err := os.ReadFile(in.PlanFilePath)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: read plan file %s: %w", in.PlanFilePath, err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		return LoadApprovedPlanOutput{}, fmt.Errorf("kernel: parse plan file %s: %w", in.PlanFilePath, err)
	}

	return LoadApprovedPlanOutput{PlanID: in.PlanID, RiskTier: approved.RiskTier(), Tasks: doc.Tasks}, nil
}

// AcquireLeaseInput is AcquireLease's input.
type AcquireLeaseInput struct {
	Resource   string
	Holder     string
	TTLSeconds int
}

// AcquireLeaseOutput is AcquireLease's output.
type AcquireLeaseOutput struct {
	Token string
}

// AcquireLease grants (or idempotently re-grants) a fencing token for
// Resource. A conflicting live holder is a deterministic ErrLeaseHeld —
// callers must not retry it blindly.
func (a *Activities) AcquireLease(ctx context.Context, in AcquireLeaseInput) (AcquireLeaseOutput, error) {
	lease, err := a.LeaseStore.Acquire(ctx, in.Resource, in.Holder, time.Duration(in.TTLSeconds)*time.Second)
	if err != nil {
		return AcquireLeaseOutput{}, err
	}
	return AcquireLeaseOutput{Token: lease.Token}, nil
}

// AcquireWorktreeInput is AcquireWorktree's input.
type AcquireWorktreeInput struct {
	WorkflowID    string
	TaskID        string
	Attempt       int
	RepoPath      string
	LeaseResource string
	LeaseToken    string
}

// AcquireWorktreeOutput is AcquireWorktree's output.
type AcquireWorktreeOutput struct {
	Path   string
	Branch string
}

// AcquireWorktree checks the fencing token is still current for
// LeaseResource, then creates an isolated worktree via internal/worktree.
// Re-execution of an already-completed acquisition (same workflow/task/
// attempt) returns the recorded receipt instead of creating a second
// worktree.
func (a *Activities) AcquireWorktree(ctx context.Context, in AcquireWorktreeInput) (AcquireWorktreeOutput, error) {
	ok, err := a.LeaseStore.Check(ctx, in.LeaseResource, in.LeaseToken)
	if err != nil {
		return AcquireWorktreeOutput{}, fmt.Errorf("kernel: check lease %s: %w", in.LeaseResource, err)
	}
	if !ok {
		return AcquireWorktreeOutput{}, fmt.Errorf("%w: fencing token no longer valid for %s", ErrLeaseHeld, in.LeaseResource)
	}

	key := IdempotencyKey{in.WorkflowID, in.TaskID, "AcquireWorktree", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (AcquireWorktreeOutput, error) {
		ws, err := a.WorktreeManager.Acquire(ctx, in.RepoPath, in.WorkflowID, in.TaskID)
		if err != nil {
			return AcquireWorktreeOutput{}, fmt.Errorf("kernel: acquire worktree %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		a.storeWorkspace(in.WorkflowID, in.TaskID, ws)
		return AcquireWorktreeOutput{Path: ws.Path, Branch: ws.Branch}, nil
	})
}

// ReleaseWorktreeInput is ReleaseWorktree's input.
type ReleaseWorktreeInput struct {
	WorkflowID string
	TaskID     string
}

// ReleaseWorktree reclaims the worktree acquired for (WorkflowID, TaskID).
// It is best-effort against the in-process cache populated by
// AcquireWorktree in the same worker process: if the worker restarted in
// between and the cache is empty, this is a no-op — the orphan is still
// eventually reclaimed by worktree.Manager.SweepOlderThan, which is this
// package's safety net rather than something workflow.go depends on
// (decision: smallest reversible option, since Workspace.Release is a
// closure that cannot cross an activity's serialization boundary).
func (a *Activities) ReleaseWorktree(_ context.Context, in ReleaseWorktreeInput) error {
	ws, ok := a.loadWorkspace(in.WorkflowID, in.TaskID)
	if !ok || ws.Release == nil {
		return nil
	}
	defer a.deleteWorkspace(in.WorkflowID, in.TaskID)
	if err := ws.Release(); err != nil {
		return fmt.Errorf("kernel: release worktree %s/%s: %w", in.WorkflowID, in.TaskID, err)
	}
	return nil
}

// ExecuteTaskInput is ExecuteTask's input.
type ExecuteTaskInput struct {
	WorkflowID    string
	TaskID        string
	Attempt       int
	ExecutorName  string
	WorkspacePath string
	Packet        executor.TaskPacket
}

// ExecuteTaskOutput is ExecuteTask's output. Failed/ErrorMessage carry the
// executor adapter's own (still untrusted) pass/fail — a task whose
// commands failed is a valid business outcome, not an activity execution
// fault, so it is reported here rather than as a Go error (that keeps
// Temporal's activity-level retry policy scoped to genuine infra faults —
// executor lookup, Prepare, Collect — not to deterministic script
// failures).
type ExecuteTaskOutput struct {
	Claimed       string
	ExitNotes     string
	Failed        bool
	ErrorMessage  string
	ArtifactPaths []string
}

// ExecuteTask runs packet inside the already-acquired worktree via the
// named executor.Adapter, heartbeating every 10s so Temporal knows the
// activity is alive during a long-running task.
func (a *Activities) ExecuteTask(ctx context.Context, in ExecuteTaskInput) (ExecuteTaskOutput, error) {
	key := IdempotencyKey{in.WorkflowID, in.TaskID, "ExecuteTask", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (ExecuteTaskOutput, error) {
		adapter, err := executor.Get(in.ExecutorName)
		if err != nil {
			return ExecuteTaskOutput{}, fmt.Errorf("kernel: execute task %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}

		ws := worktree.Workspace{Path: in.WorkspacePath}
		if err := adapter.Prepare(ctx, ws, in.Packet); err != nil {
			return ExecuteTaskOutput{}, fmt.Errorf("kernel: prepare task %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}

		stopHeartbeat := startHeartbeat(ctx)
		summary, runErr := adapter.Run(ctx)
		stopHeartbeat()

		out := ExecuteTaskOutput{Claimed: summary.Claimed, ExitNotes: summary.ExitNotes}
		if runErr != nil {
			out.Failed = true
			out.ErrorMessage = runErr.Error()
			return out, nil
		}

		artifacts, err := adapter.Collect(ctx)
		if err != nil {
			return ExecuteTaskOutput{}, fmt.Errorf("kernel: collect task %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		out.ArtifactPaths = artifacts.Paths
		return out, nil
	})
}

// startHeartbeat records an activity heartbeat every heartbeatInterval
// until the returned stop function is called or ctx is done. Called from
// inside an activity, so activity.RecordHeartbeat is safe here (it would
// not be safe from workflow code).
func startHeartbeat(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "running")
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(done) }
}

// ValidateTaskInput is ValidateTask's input.
type ValidateTaskInput struct {
	ExecuteFailed bool
}

// ValidateTaskOutput is ValidateTask's output.
type ValidateTaskOutput struct {
	Validated bool
	Reason    string
}

// ValidateTask is a STUB pending Task 13 (internal/verify): it checks only
// whether ExecuteTask's adapter run reported failure — it does not
// independently re-run or verify commands against recorded evidence, and
// it must not be mistaken for Task 13's honest, evidence-based validator.
// TODO(Task 13): replace this body with a call into internal/verify.Runner
// and classify from its CommandRecords, not from the executor's own
// self-report.
func (a *Activities) ValidateTask(_ context.Context, in ValidateTaskInput) (ValidateTaskOutput, error) {
	if in.ExecuteFailed {
		return ValidateTaskOutput{Validated: false, Reason: "verification-failed"}, nil
	}
	return ValidateTaskOutput{Validated: true}, nil
}

// RecordEvidenceInput is RecordEvidence's input.
type RecordEvidenceInput struct {
	WorkflowID    string
	TaskID        string
	Attempt       int
	WorkspacePath string
	ArtifactPaths []string
	ExecuteFailed bool
}

// RecordEvidenceOutput is RecordEvidence's output.
type RecordEvidenceOutput struct {
	BundleID string
}

// RecordEvidence hashes the artifacts ExecuteTask collected, builds an
// evidence.Manifest, and persists it via the evidence.Store. Bundles are
// content-addressed (Task 11): re-recording identical evidence returns the
// existing bundle's ID rather than erroring.
func (a *Activities) RecordEvidence(ctx context.Context, in RecordEvidenceInput) (RecordEvidenceOutput, error) {
	key := IdempotencyKey{in.WorkflowID, in.TaskID, "RecordEvidence", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (RecordEvidenceOutput, error) {
		artifacts := make([]evidence.ArtifactRef, 0, len(in.ArtifactPaths))
		for _, p := range in.ArtifactPaths {
			full := filepath.Join(in.WorkspacePath, p)
			sum, size, err := hashArtifact(full)
			if err != nil {
				return RecordEvidenceOutput{}, fmt.Errorf("kernel: hash artifact %s: %w", p, err)
			}
			artifacts = append(artifacts, evidence.ArtifactRef{Path: p, SHA256: sum, Bytes: size})
		}

		exitCode := 0
		if in.ExecuteFailed {
			exitCode = 1
		}
		manifest := evidence.Manifest{
			WorkflowID: in.WorkflowID,
			TaskID:     in.TaskID,
			Commands: []evidence.CommandRecord{{
				Cmd:      "executor.Run", // coarse record: Task 13's Runner replaces this with per-command records.
				ExitCode: exitCode,
			}},
			Artifacts: artifacts,
			CreatedAt: time.Now().UTC(),
		}

		id, err := manifest.DigestHex()
		if err != nil {
			return RecordEvidenceOutput{}, fmt.Errorf("kernel: compute evidence digest %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}

		putID, err := a.EvidenceStore.Put(evidence.Bundle{Manifest: manifest, Dir: in.WorkspacePath})
		if err != nil {
			if errors.Is(err, evidence.ErrBundleExists) {
				return RecordEvidenceOutput{BundleID: id}, nil
			}
			return RecordEvidenceOutput{}, fmt.Errorf("kernel: record evidence %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		return RecordEvidenceOutput{BundleID: putID}, nil
	})
}

func hashArtifact(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// AppendTransitionInput is AppendTransition's input. TaskID scopes the
// idempotency key only — it is not part of the persisted state.Transition
// itself.
type AppendTransitionInput struct {
	WorkflowID string
	TaskID     string
	Attempt    int
	Transition state.Transition
}

// AppendTransitionOutput is AppendTransition's output.
type AppendTransitionOutput struct {
	Seq int64
}

// AppendTransition durably persists one canonical state.Transition to the
// workflow_transitions stream — the source of Task 14's projection.
func (a *Activities) AppendTransition(ctx context.Context, in AppendTransitionInput) (AppendTransitionOutput, error) {
	key := IdempotencyKey{in.WorkflowID, in.TaskID, "AppendTransition", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (AppendTransitionOutput, error) {
		seq, err := a.TransitionStore.Append(ctx, in.WorkflowID, in.Transition)
		if err != nil {
			return AppendTransitionOutput{}, fmt.Errorf("kernel: append transition %s: %w", in.WorkflowID, err)
		}
		return AppendTransitionOutput{Seq: seq}, nil
	})
}
