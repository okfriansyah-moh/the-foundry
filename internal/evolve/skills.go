package evolve

import (
	"fmt"
	"sync"
	"time"
)

// SkillVersion is one immutable version of a skill or prompt. Promotion never
// mutates a version in place — it appends a new one and retains the previous
// (reversibility, an L1 condition).
type SkillVersion struct {
	SkillID     string
	Version     int
	Prompt      string
	Permissions []string // capabilities this version is allowed to use
	DataClasses []string // data classes this version may touch
	BudgetUSD   float64  // per-invocation budget ceiling this version declares
}

// SkillRegistry holds the version history of each skill. It is the source of
// truth L1 promotion bumps, and it always retains previous versions so any
// promotion is reversible in one step (Rollback).
type SkillRegistry struct {
	mu       sync.RWMutex
	versions map[string][]SkillVersion
}

// NewSkillRegistry returns an empty registry.
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{versions: map[string][]SkillVersion{}}
}

// Register seeds a skill's initial (v1) version.
func (r *SkillRegistry) Register(v SkillVersion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v = cloneSkillVersion(v)
	if v.Version == 0 {
		v.Version = 1
	}
	r.versions[v.SkillID] = []SkillVersion{v}
}

// Current returns the latest version of skillID.
func (r *SkillRegistry) Current(skillID string) (SkillVersion, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hist := r.versions[skillID]
	if len(hist) == 0 {
		return SkillVersion{}, false
	}
	return cloneSkillVersion(hist[len(hist)-1]), true
}

// History returns the full retained version list for skillID (oldest first).
func (r *SkillRegistry) History(skillID string) []SkillVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hist := r.versions[skillID]
	out := make([]SkillVersion, len(hist))
	for i := range hist {
		out[i] = cloneSkillVersion(hist[i])
	}
	return out
}

// promote appends proposed as the next version, retaining all previous ones.
func (r *SkillRegistry) promote(skillID string, proposed SkillVersion) SkillVersion {
	r.mu.Lock()
	defer r.mu.Unlock()
	hist := r.versions[skillID]
	cur := hist[len(hist)-1]
	proposed = cloneSkillVersion(proposed)
	proposed.SkillID = skillID
	proposed.Version = cur.Version + 1
	r.versions[skillID] = append(hist, proposed)
	return cloneSkillVersion(proposed)
}

// Rollback reverts skillID to its immediately-previous version by appending a
// copy of it as a new version (so history stays append-only and auditable).
// It is the one-command reversibility guarantee. It errors if there is no
// previous version to roll back to.
func (r *SkillRegistry) Rollback(skillID string) (SkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hist := r.versions[skillID]
	if len(hist) < 2 {
		return SkillVersion{}, fmt.Errorf("evolve: skill %s has no previous version to roll back to", skillID)
	}
	prev := cloneSkillVersion(hist[len(hist)-2])
	prev.SkillID = skillID
	prev.Version = hist[len(hist)-1].Version + 1
	r.versions[skillID] = append(hist, prev)
	return cloneSkillVersion(prev), nil
}

func (r *SkillRegistry) clone() *SkillRegistry {
	copyRegistry := NewSkillRegistry()
	if r == nil {
		return copyRegistry
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for skillID, history := range r.versions {
		copyHistory := make([]SkillVersion, len(history))
		for index := range history {
			copyHistory[index] = cloneSkillVersion(history[index])
		}
		copyRegistry.versions[skillID] = copyHistory
	}
	return copyRegistry
}

func cloneSkillVersion(version SkillVersion) SkillVersion {
	version.Permissions = append([]string(nil), version.Permissions...)
	version.DataClasses = append([]string(nil), version.DataClasses...)
	return version
}

// GoldenTask is one deterministic eval task: Check returns whether a version
// satisfies it. No randomness, no LLM — the same version always scores the
// same.
type GoldenTask struct {
	Name  string
	Check func(v SkillVersion) bool
}

// GoldenSuite is a skill's deterministic eval suite (the eval harness). Score
// is the fraction of golden tasks the version passes, in [0,1].
type GoldenSuite struct {
	Tasks []GoldenTask
}

// Score returns v's deterministic pass fraction over the suite.
func (s GoldenSuite) Score(v SkillVersion) float64 {
	if len(s.Tasks) == 0 {
		return 0
	}
	passed := 0
	for _, t := range s.Tasks {
		if t.Check(v) {
			passed++
		}
	}
	return float64(passed) / float64(len(s.Tasks))
}

// L1Candidate is a proposed skill/prompt evolution.
type L1Candidate struct {
	Base     SkillVersion
	Proposed SkillVersion
	// Profile is the profile the candidate is evaluated for. Only "personal"
	// may auto-promote; every other profile (e.g. org) is proposal-only.
	Profile string
}

// L1Stages carries the shadow/canary outcomes for a candidate. A candidate
// stays quarantined (never on the critical path) until ShadowPass is true.
type L1Stages struct {
	ShadowPass bool
	CanaryPass bool
}

// L1ConditionFailure names which drift-governance L1 condition a candidate
// violated, or "" if it passes all of them.
type L1ConditionFailure string

const (
	L1OK                L1ConditionFailure = ""
	L1NewPermission     L1ConditionFailure = "new-permission"
	L1NewDataClass      L1ConditionFailure = "new-data-class"
	L1BudgetIncrease    L1ConditionFailure = "budget-increase"
	L1NotReversible     L1ConditionFailure = "not-reversible"
	L1QualityRegression L1ConditionFailure = "quality-regression"
)

// CheckL1Conditions applies the deterministic drift-governance L1 gates: no
// new permissions, no new data class, no budget increase, and reversibility
// (a base version to retain). It does not consider eval scores — that is a
// separate gate in the pipeline.
func CheckL1Conditions(cand L1Candidate) L1ConditionFailure {
	// Reversibility first: without a retained base version there is nothing to
	// roll back to, and the subset/budget comparisons below are meaningless.
	if cand.Base.SkillID == "" || cand.Base.Version == 0 {
		return L1NotReversible
	}
	if !subset(cand.Proposed.Permissions, cand.Base.Permissions) {
		return L1NewPermission
	}
	if !subset(cand.Proposed.DataClasses, cand.Base.DataClasses) {
		return L1NewDataClass
	}
	if cand.Proposed.BudgetUSD > cand.Base.BudgetUSD {
		return L1BudgetIncrease
	}
	return L1OK
}

// L1Outcome is the result of running a candidate through the L1 pipeline.
type L1Outcome struct {
	Record           PromotionRecord
	ConditionFailure L1ConditionFailure
	// Applied is true only when the registry version was actually bumped
	// (personal profile, all gates clean). Org/other profiles never apply.
	Applied bool
	// ProposalOnly is true when the candidate cleared every gate but is on a
	// non-personal profile, so it is recorded as a Tier-H proposal only.
	ProposalOnly bool
}

// L1Pipeline runs generate→evaluate→quarantine→shadow→canary→promote for
// skill/prompt (L1) changes (docs/PLAN.md Task 77 / EVO-04). Personal-profile
// candidates that clear every gate are promoted (registry version bump,
// previous retained, inside the drift budget); org candidates are
// proposal-only.
type L1Pipeline struct {
	Registry *SkillRegistry
	Suite    GoldenSuite
	Limits   ChangeBudgetLimits
	Window   BudgetWindow
	Now      func() time.Time
}

func (p L1Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

// Evaluate runs cand through every L1 gate in order and returns the outcome.
// Order: L1 condition gates → eval (no quality regression) → quarantine/shadow
// → canary → drift-budget/freeze → profile (apply vs propose-only).
func (p L1Pipeline) Evaluate(cand L1Candidate, stages L1Stages) L1Outcome {
	return p.evaluate(cand, stages, true)
}

// evaluate runs the L1 gates with an explicit process-latch policy. Durable
// activation callers pass false because their transaction-backed freeze guard
// is the cross-process authority; ordinary Task 77 callers retain the hot
// in-process latch behavior through Evaluate.
func (p L1Pipeline) evaluate(cand L1Candidate, stages L1Stages, checkProcessFreeze bool) L1Outcome {
	rec := PromotionRecord{
		Tunable:       cand.Proposed.SkillID,
		PreviousValue: float64(cand.Base.Version),
		PromotedValue: float64(cand.Base.Version), // stays at base until promoted
		RollbackRef:   fmt.Sprintf("%s@v%d", cand.Base.SkillID, cand.Base.Version),
		Level:         LevelL1,
		CreatedAt:     p.now(),
	}

	// (1) Deterministic L1 condition gates.
	if fail := CheckL1Conditions(cand); fail != L1OK {
		rec.Stage = StageRejected
		return L1Outcome{Record: rec, ConditionFailure: fail}
	}

	// (2) Eval: the proposed version must not regress the golden-task score.
	if p.Suite.Score(cand.Proposed) < p.Suite.Score(cand.Base) {
		rec.Stage = StageRejected
		return L1Outcome{Record: rec, ConditionFailure: L1QualityRegression}
	}

	// (3) Quarantine until shadow-clean — never on the critical path first.
	if !stages.ShadowPass {
		rec.Stage = StageQuarantined
		return L1Outcome{Record: rec}
	}

	// (4) Canary.
	if !stages.CanaryPass {
		rec.Stage = StageReverted
		return L1Outcome{Record: rec}
	}

	// (5) Drift budget / freeze: promotion may only happen inside budget.
	if checkProcessFreeze && IsFrozen() {
		rec.Stage = StageQuarantined
		return L1Outcome{Record: rec}
	}
	projected := p.Window
	projected.Promotions++
	if len(projected.Breaches(p.Limits)) > 0 {
		rec.Stage = StageQuarantined
		return L1Outcome{Record: rec}
	}

	// (6) Profile: only personal auto-promotes; others are proposal-only (H).
	if cand.Profile != "personal" {
		rec.Stage = StageProposed
		return L1Outcome{Record: rec, ProposalOnly: true}
	}

	promoted := p.Registry.promote(cand.Proposed.SkillID, cand.Proposed)
	rec.Stage = StagePromoted
	rec.PromotedValue = float64(promoted.Version)
	return L1Outcome{Record: rec, Applied: true}
}

// subset reports whether every element of a is present in b.
func subset(a, b []string) bool {
	set := map[string]struct{}{}
	for _, v := range b {
		set[v] = struct{}{}
	}
	for _, v := range a {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}
