package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ---- Seams -----------------------------------------------------------------
//
// Every authority-bearing step the pipeline crosses belongs to another card, so
// it is reached through a narrow seam. Production wires the real packages
// (opportunity, spec, admission, approval, mission start); tests wire
// deterministic fakes and cassettes so the whole pipeline runs with zero
// network and no database.

// Validator runs opportunity validation (stage 2) and returns a verdict. It is
// the ONLY stage that may spend the research cap. A REJECT or VALIDATE-MORE
// verdict ends the run by design.
type Validator interface {
	Validate(ctx context.Context, in ValidateInput) (ValidateOutput, error)
}

// ValidateInput carries the idea and the research cap the validator may spend.
type ValidateInput struct {
	RunID          string
	Idea           string
	ResearchCapUSD float64
}

// ValidateOutput is the verdict, the reasons a BUILD was not reached, the
// operator's next actions for a terminal outcome, and the actual research
// spend.
type ValidateOutput struct {
	Verdict     string   // "BUILD" | "VALIDATE-MORE" | "REJECT"
	Blockers    []string // named reject reasons / unmet thresholds
	NextActions []string // printed on a terminal-by-design outcome
	SpentUSD    float64
	Digest      string // opportunity/scorecard digest, for provenance
}

// SpecSynthesizer turns an idea into a specification (stage 3).
type SpecSynthesizer interface {
	Synthesize(ctx context.Context, in SynthInput) (SynthOutput, error)
}

// SynthInput carries the idea being synthesized.
type SynthInput struct {
	RunID string
	Idea  string
}

// SynthOutput is the serialized specification and its digest.
type SynthOutput struct {
	SpecJSON []byte
	Digest   string
}

// PlanGenerator turns a specification into an executable PLAN (stage 4). The
// generated plan never self-classifies (C6).
type PlanGenerator interface {
	Generate(ctx context.Context, in PlanGenInput) (PlanGenOutput, error)
}

// PlanGenInput carries the specification produced by stage 3 and the mission
// envelope the plan must budget within.
type PlanGenInput struct {
	RunID       string
	PlanID      string
	Title       string
	SpecJSON    []byte
	EnvelopeUSD float64
}

// PlanGenOutput is the serialized PLAN document (markdown+front matter).
type PlanGenOutput struct {
	PlanBytes []byte
	PlanID    string
}

// Admitter classifies a PLAN into an admission tier (stage 5). This is the
// kernel's deterministic classifier; the pipeline only reads its verdict.
type Admitter interface {
	Classify(ctx context.Context, in AdmitInput) (AdmitOutput, error)
}

// AdmitInput carries the serialized PLAN to classify.
type AdmitInput struct {
	RunID     string
	PlanBytes []byte
}

// AdmitOutput is the classified tier and whether it demands strong-auth.
type AdmitOutput struct {
	Tier               string
	PolicyDigest       string
	RequiresStrongAuth bool
}

// Approver approves an admitted plan (stage 6). It MUST refuse to approve an
// H-tier / strong-auth-required plan (the pipeline never self-approves, C6/C12)
// by returning ErrStrongAuthRequired.
type Approver interface {
	Approve(ctx context.Context, in ApproveInput) (ApproveOutput, error)
}

// ApproveInput carries the plan and its classified tier.
type ApproveInput struct {
	RunID     string
	PlanBytes []byte
	Tier      string
}

// ApproveOutput references the recorded ApprovedPlan.
type ApproveOutput struct {
	ApprovalRef string
}

// ErrStrongAuthRequired signals that a plan requires strong-auth approval and
// the pipeline must pause rather than self-approve.
var ErrStrongAuthRequired = errors.New("intake: strong-auth approval required")

// ReadinessChecker verifies the mission-setup ceremony (C17) before an
// unattended mission may start (stage 7). It derives the answers it can and
// reports the ones it cannot rather than fabricating them.
type ReadinessChecker interface {
	Check(ctx context.Context, in ReadinessInput) (ReadinessOutput, error)
}

// ReadinessInput carries the run and its approved plan.
type ReadinessInput struct {
	RunID       string
	ApprovalRef string
}

// ReadinessOutput reports whether the ceremony is satisfied and, if not, which
// answers the operator must still supply.
type ReadinessOutput struct {
	Ready       bool
	Missing     []string
	ArtifactRef string
}

// MissionStarter starts the MissionLoop for an approved, ready plan (stage 7).
type MissionStarter interface {
	Start(ctx context.Context, in StartMissionInput) (StartMissionOutput, error)
}

// StartMissionInput carries the approved plan reference and the mission
// envelope.
type StartMissionInput struct {
	RunID       string
	ApprovalRef string
	EnvelopeUSD float64
}

// StartMissionOutput references the started mission.
type StartMissionOutput struct {
	MissionID string
}

// BudgetGate enforces the fail-closed budget rule (Task 119 owns the rule; this
// pipeline obeys it). It refuses to admit a spend that would breach the
// envelope, and refuses entirely when there is no envelope.
type BudgetGate interface {
	Admit(ctx context.Context, runID string, caps Caps, alreadySpentUSD, requestUSD float64) error
}

// DefaultBudgetGate is the fail-closed default: no envelope refuses; a spend
// that would exceed the envelope refuses.
type DefaultBudgetGate struct{}

// Admit implements BudgetGate.
func (DefaultBudgetGate) Admit(_ context.Context, _ string, caps Caps, alreadySpentUSD, requestUSD float64) error {
	if caps.EnvelopeUSD <= 0 {
		return fmt.Errorf("intake: no budget envelope: unattended intake refuses to spend (Task 119, C19/C24)")
	}
	if alreadySpentUSD+requestUSD > caps.EnvelopeUSD {
		return fmt.Errorf("intake: spend %.4f would breach envelope %.4f (already spent %.4f)", requestUSD, caps.EnvelopeUSD, alreadySpentUSD)
	}
	return nil
}

// ---- Pipeline --------------------------------------------------------------

// Deps bundles the store, the seams, the budget gate and a clock. Every seam is
// required except Budget (defaults to DefaultBudgetGate) and Clock (defaults to
// time.Now).
type Deps struct {
	Store     Store
	Validator Validator
	Synth     SpecSynthesizer
	PlanGen   PlanGenerator
	Admitter  Admitter
	Approver  Approver
	Readiness ReadinessChecker
	Starter   MissionStarter
	Budget    BudgetGate
	Clock     func() time.Time
}

// Pipeline drives an intake run through its stages.
type Pipeline struct {
	deps Deps
}

// NewPipeline validates the dependency set and returns a Pipeline.
func NewPipeline(d Deps) (*Pipeline, error) {
	if d.Store == nil {
		return nil, errors.New("intake: store is required")
	}
	if d.Validator == nil || d.Synth == nil || d.PlanGen == nil ||
		d.Admitter == nil || d.Approver == nil || d.Readiness == nil || d.Starter == nil {
		return nil, errors.New("intake: all stage seams are required")
	}
	if d.Budget == nil {
		d.Budget = DefaultBudgetGate{}
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	return &Pipeline{deps: d}, nil
}

// StartInput is the parameters of a new intake run.
type StartInput struct {
	RunID  string // optional; minted when empty
	Idea   string
	Budget float64
	Origin Origin
}

// Start creates a new run at IDEA_RECORDED and advances it as far as it can go
// in one call (to a terminal or a pause). It returns the final run state.
func (p *Pipeline) Start(ctx context.Context, in StartInput) (Run, error) {
	if in.Idea == "" {
		return Run{}, errors.New("intake: idea text is required")
	}
	id := in.RunID
	if id == "" {
		var err error
		if id, err = newID("intake"); err != nil {
			return Run{}, err
		}
	}
	now := p.deps.Clock().UTC()
	if in.Origin.Channel == "" {
		in.Origin.Channel = "cli"
	}
	run := Run{
		ID:           id,
		Idea:         in.Idea,
		Caps:         CapsFromBudget(in.Budget),
		Origin:       in.Origin,
		CurrentStage: StageIdeaRecorded,
		Status:       StatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	created, err := p.deps.Store.CreateRun(ctx, run)
	if err != nil {
		return Run{}, err
	}
	// Stage 1 output is the recorded idea, keyed by its own digest.
	if err := p.recordStage(ctx, created.ID, StageIdeaRecorded, digest(created.Idea), mustJSON(map[string]string{"idea_digest": digest(created.Idea)}), 0); err != nil {
		return Run{}, err
	}
	return p.Resume(ctx, created.ID)
}

// Resume advances an existing run from wherever it stopped. It is safe to call
// repeatedly: completed stages return their recorded output without re-spending
// or re-calling a provider.
func (p *Pipeline) Resume(ctx context.Context, runID string) (Run, error) {
	run, err := p.deps.Store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	if run.Status == StatusDone {
		return run, nil
	}
	for _, stage := range forwardStages {
		if stage == StageIdeaRecorded {
			continue
		}
		terminal, paused, err := p.runStage(ctx, &run, stage)
		if err != nil {
			return run, err
		}
		if terminal || paused {
			return run, nil
		}
	}
	// Reached MISSION_STARTED without a pause.
	run.CurrentStage = StageMissionStarted
	run.Status = StatusDone
	if err := p.saveRun(ctx, &run); err != nil {
		return run, err
	}
	return run, nil
}

// mustJSON marshals v, panicking only on a programmer error (a value this
// package fully controls).
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("intake: marshal artifact: %v", err))
	}
	return b
}

// recordStage appends a stage record and is idempotent on (run, stage).
func (p *Pipeline) recordStage(ctx context.Context, runID string, stage Stage, inputDigest string, output []byte, cost float64) error {
	return p.deps.Store.RecordStage(ctx, StageRecord{
		RunID:       runID,
		Stage:       stage,
		InputDigest: inputDigest,
		Output:      output,
		CostUSD:     cost,
		CreatedAt:   p.deps.Clock().UTC(),
	})
}

// saveRun stamps updated_at and persists the run's mutable fields.
func (p *Pipeline) saveRun(ctx context.Context, run *Run) error {
	run.UpdatedAt = p.deps.Clock().UTC()
	return p.deps.Store.UpdateRun(ctx, *run)
}
