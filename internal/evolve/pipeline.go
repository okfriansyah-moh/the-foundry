package evolve

import (
	"fmt"
	"time"
)

type Candidate struct {
	Name     string
	Current  float64
	Proposed float64
}

type Evaluation struct {
	ReplayPass bool
	ShadowPass bool
	CanaryPass bool
}

type L0Pipeline struct {
	Registry TunableRegistry
	Now      func() time.Time
}

func (p L0Pipeline) Evaluate(candidate Candidate, evaluation Evaluation) (PromotionRecord, error) {
	if !p.Registry.InBounds(candidate.Name, candidate.Proposed) {
		return PromotionRecord{Tunable: candidate.Name, PreviousValue: candidate.Current, PromotedValue: candidate.Current, RollbackRef: candidate.Name + "@" + fmt.Sprintf("%g", candidate.Current), Stage: StageRejected, Level: LevelL0, CreatedAt: p.now()}, fmt.Errorf("evolve: candidate %s out of bounds", candidate.Name)
	}
	record := PromotionRecord{Tunable: candidate.Name, PreviousValue: candidate.Current, PromotedValue: candidate.Proposed, RollbackRef: candidate.Name + "@" + fmt.Sprintf("%g", candidate.Current), Level: LevelL0, CreatedAt: p.now()}
	switch {
	case !evaluation.ReplayPass:
		record.Stage = StageRejected
	case !evaluation.ShadowPass:
		record.Stage = StageReverted
	case !evaluation.CanaryPass:
		record.Stage = StageReverted
	default:
		record.Stage = StagePromoted
	}
	return record, nil
}

func (p L0Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
