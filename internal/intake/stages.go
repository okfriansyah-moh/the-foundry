package intake

import (
	"context"
	"encoding/json"
	"fmt"
)

// Stage artifact shapes, serialized into StageRecord.Output. Keeping them here
// lets Resume reconstruct a run's intermediate state after a restart.

type validatedArtifact struct {
	Verdict     string   `json:"verdict"`
	Blockers    []string `json:"blockers,omitempty"`
	NextActions []string `json:"next_actions,omitempty"`
	SpentUSD    float64  `json:"spent_usd"`
	Digest      string   `json:"digest,omitempty"`
}

type specArtifact struct {
	SpecJSON json.RawMessage `json:"spec"`
	Digest   string          `json:"digest"`
}

type planArtifact struct {
	PlanID    string `json:"plan_id"`
	PlanBytes []byte `json:"plan_bytes"`
}

type admitArtifact struct {
	Tier               string `json:"tier"`
	PolicyDigest       string `json:"policy_digest,omitempty"`
	RequiresStrongAuth bool   `json:"requires_strong_auth"`
}

type approveArtifact struct {
	ApprovalRef string `json:"approval_ref"`
}

type missionArtifact struct {
	MissionID string `json:"mission_id"`
}

// runStage executes one forward stage, honouring idempotency (a recorded stage
// is loaded, not re-run). It returns (terminal, paused, error): terminal means
// the run ended by design (built nothing, or mission started); paused means the
// run stopped awaiting an external action.
func (p *Pipeline) runStage(ctx context.Context, run *Run, stage Stage) (terminal, paused bool, err error) {
	// Idempotency: if this stage is already recorded, restore its effect on the
	// run and continue without re-spending or re-calling a provider.
	if rec, ok, gerr := p.deps.Store.GetStage(ctx, run.ID, stage); gerr != nil {
		return false, false, gerr
	} else if ok {
		return p.replayStage(run, stage, rec)
	}

	switch stage {
	case StageOpportunityValidated:
		return p.stageValidate(ctx, run)
	case StageSpecSynthesized:
		return p.stageSynthesize(ctx, run)
	case StagePlanGenerated:
		return p.stagePlan(ctx, run)
	case StageAdmitted:
		return p.stageAdmit(ctx, run)
	case StageApproved:
		return p.stageApprove(ctx, run)
	case StageMissionStarted:
		return p.stageStart(ctx, run)
	default:
		return false, false, fmt.Errorf("intake: unknown forward stage %q", stage)
	}
}

// replayStage restores a run's state from an already-recorded stage so a resume
// or a re-run produces the same final artifacts without duplicating work.
func (p *Pipeline) replayStage(run *Run, stage Stage, rec StageRecord) (terminal, paused bool, err error) {
	switch stage {
	case StageOpportunityValidated:
		var a validatedArtifact
		if err := json.Unmarshal(rec.Output, &a); err != nil {
			return false, false, fmt.Errorf("intake: decode validated artifact: %w", err)
		}
		switch a.Verdict {
		case "REJECT", "VALIDATE-MORE":
			// Terminal-by-design was already recorded; keep the run there.
			return true, false, nil
		}
		return false, false, nil
	case StageMissionStarted:
		var a missionArtifact
		if err := json.Unmarshal(rec.Output, &a); err != nil {
			return false, false, fmt.Errorf("intake: decode mission artifact: %w", err)
		}
		run.MissionID = a.MissionID
		return false, false, nil
	default:
		// Spec/plan/admit/approve records carry no run-level side effect to
		// restore; their output is consumed by the next stage via loadOutput.
		return false, false, nil
	}
}

// stageValidate runs opportunity validation. It is the only stage that spends
// the research cap, and its REJECT/VALIDATE-MORE verdicts end the run by design.
func (p *Pipeline) stageValidate(ctx context.Context, run *Run) (terminal, paused bool, err error) {
	// Budget: the envelope must exist and admit the research cap before we
	// spend anything (Task 119, obeyed here).
	if err := p.deps.Budget.Admit(ctx, run.ID, run.Caps, run.SpentUSD, run.Caps.ResearchCapUSD); err != nil {
		return false, false, err
	}
	out, err := p.deps.Validator.Validate(ctx, ValidateInput{
		RunID:          run.ID,
		Idea:           run.Idea,
		ResearchCapUSD: run.Caps.ResearchCapUSD,
	})
	if err != nil {
		return false, false, fmt.Errorf("intake: opportunity validation: %w", err)
	}
	run.SpentUSD += out.SpentUSD
	art := validatedArtifact{
		Verdict:     out.Verdict,
		Blockers:    out.Blockers,
		NextActions: out.NextActions,
		SpentUSD:    out.SpentUSD,
		Digest:      out.Digest,
	}
	if err := p.recordStage(ctx, run.ID, StageOpportunityValidated, digest(run.Idea), mustJSON(art), out.SpentUSD); err != nil {
		return false, false, err
	}
	switch out.Verdict {
	case "REJECT":
		run.CurrentStage = StageOpportunityRejected
		run.Status = StatusDone
		if err := p.saveRun(ctx, run); err != nil {
			return false, false, err
		}
		return true, false, nil
	case "VALIDATE-MORE":
		run.CurrentStage = StageOpportunityValidationRequired
		run.Status = StatusDone
		if err := p.saveRun(ctx, run); err != nil {
			return false, false, err
		}
		return true, false, nil
	case "BUILD":
		run.CurrentStage = StageOpportunityValidated
		return false, false, p.saveRun(ctx, run)
	default:
		return false, false, fmt.Errorf("intake: unknown verdict %q", out.Verdict)
	}
}

// stageSynthesize turns the idea into a specification.
func (p *Pipeline) stageSynthesize(ctx context.Context, run *Run) (terminal, paused bool, err error) {
	out, err := p.deps.Synth.Synthesize(ctx, SynthInput{RunID: run.ID, Idea: run.Idea})
	if err != nil {
		return false, false, fmt.Errorf("intake: spec synthesis: %w", err)
	}
	art := specArtifact{SpecJSON: json.RawMessage(out.SpecJSON), Digest: out.Digest}
	if err := p.recordStage(ctx, run.ID, StageSpecSynthesized, digest(run.Idea), mustJSON(art), 0); err != nil {
		return false, false, err
	}
	run.CurrentStage = StageSpecSynthesized
	return false, false, p.saveRun(ctx, run)
}

// stagePlan generates an executable, least-privilege PLAN from the spec.
func (p *Pipeline) stagePlan(ctx context.Context, run *Run) (terminal, paused bool, err error) {
	var spec specArtifact
	if err := p.loadOutput(ctx, run.ID, StageSpecSynthesized, &spec); err != nil {
		return false, false, err
	}
	out, err := p.deps.PlanGen.Generate(ctx, PlanGenInput{
		RunID:       run.ID,
		PlanID:      run.ID + "-plan",
		Title:       "Intake mission " + run.ID,
		SpecJSON:    spec.SpecJSON,
		EnvelopeUSD: run.Caps.EnvelopeUSD,
	})
	if err != nil {
		return false, false, fmt.Errorf("intake: plan generation: %w", err)
	}
	art := planArtifact{PlanID: out.PlanID, PlanBytes: out.PlanBytes}
	if err := p.recordStage(ctx, run.ID, StagePlanGenerated, spec.Digest, mustJSON(art), 0); err != nil {
		return false, false, err
	}
	run.CurrentStage = StagePlanGenerated
	return false, false, p.saveRun(ctx, run)
}

// stageAdmit classifies the generated PLAN into an admission tier.
func (p *Pipeline) stageAdmit(ctx context.Context, run *Run) (terminal, paused bool, err error) {
	var pl planArtifact
	if err := p.loadOutput(ctx, run.ID, StagePlanGenerated, &pl); err != nil {
		return false, false, err
	}
	out, err := p.deps.Admitter.Classify(ctx, AdmitInput{RunID: run.ID, PlanBytes: pl.PlanBytes})
	if err != nil {
		return false, false, fmt.Errorf("intake: admission: %w", err)
	}
	art := admitArtifact{Tier: out.Tier, PolicyDigest: out.PolicyDigest, RequiresStrongAuth: out.RequiresStrongAuth}
	if err := p.recordStage(ctx, run.ID, StageAdmitted, digest(string(pl.PlanBytes)), mustJSON(art), 0); err != nil {
		return false, false, err
	}
	run.CurrentStage = StageAdmitted
	return false, false, p.saveRun(ctx, run)
}

// stageApprove approves an admitted plan. An H-tier / strong-auth-required plan
// pauses awaiting strong-auth — the pipeline never self-approves (C6/C12).
func (p *Pipeline) stageApprove(ctx context.Context, run *Run) (terminal, paused bool, err error) {
	var adm admitArtifact
	if err := p.loadOutput(ctx, run.ID, StageAdmitted, &adm); err != nil {
		return false, false, err
	}
	var pl planArtifact
	if err := p.loadOutput(ctx, run.ID, StagePlanGenerated, &pl); err != nil {
		return false, false, err
	}
	if adm.RequiresStrongAuth {
		run.CurrentStage = StageAwaitingStrongAuth
		run.Status = StatusPaused
		return false, true, p.saveRun(ctx, run)
	}
	out, err := p.deps.Approver.Approve(ctx, ApproveInput{RunID: run.ID, PlanBytes: pl.PlanBytes, Tier: adm.Tier})
	if err != nil {
		if err == ErrStrongAuthRequired {
			run.CurrentStage = StageAwaitingStrongAuth
			run.Status = StatusPaused
			return false, true, p.saveRun(ctx, run)
		}
		return false, false, fmt.Errorf("intake: approval: %w", err)
	}
	art := approveArtifact{ApprovalRef: out.ApprovalRef}
	if err := p.recordStage(ctx, run.ID, StageApproved, digest(string(pl.PlanBytes), adm.Tier), mustJSON(art), 0); err != nil {
		return false, false, err
	}
	run.CurrentStage = StageApproved
	return false, false, p.saveRun(ctx, run)
}

// stageStart starts the MissionLoop once the ceremony (C17) is satisfied. A
// missing ceremony answer pauses the run rather than fabricating it.
func (p *Pipeline) stageStart(ctx context.Context, run *Run) (terminal, paused bool, err error) {
	var appr approveArtifact
	if err := p.loadOutput(ctx, run.ID, StageApproved, &appr); err != nil {
		return false, false, err
	}
	ready, err := p.deps.Readiness.Check(ctx, ReadinessInput{RunID: run.ID, ApprovalRef: appr.ApprovalRef})
	if err != nil {
		return false, false, fmt.Errorf("intake: readiness: %w", err)
	}
	if !ready.Ready {
		run.CurrentStage = StageAwaitingReadiness
		run.Status = StatusPaused
		return false, true, p.saveRun(ctx, run)
	}
	out, err := p.deps.Starter.Start(ctx, StartMissionInput{
		RunID:       run.ID,
		ApprovalRef: appr.ApprovalRef,
		EnvelopeUSD: run.Caps.EnvelopeUSD,
	})
	if err != nil {
		return false, false, fmt.Errorf("intake: mission start: %w", err)
	}
	run.MissionID = out.MissionID
	art := missionArtifact{MissionID: out.MissionID}
	if err := p.recordStage(ctx, run.ID, StageMissionStarted, digest(appr.ApprovalRef), mustJSON(art), 0); err != nil {
		return false, false, err
	}
	run.CurrentStage = StageMissionStarted
	run.Status = StatusDone
	return false, false, p.saveRun(ctx, run)
}

// loadOutput decodes a previously recorded stage's output artifact into v.
func (p *Pipeline) loadOutput(ctx context.Context, runID string, stage Stage, v any) error {
	rec, ok, err := p.deps.Store.GetStage(ctx, runID, stage)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("intake: stage %s output missing for run %s", stage, runID)
	}
	if err := json.Unmarshal(rec.Output, v); err != nil {
		return fmt.Errorf("intake: decode %s output: %w", stage, err)
	}
	return nil
}
