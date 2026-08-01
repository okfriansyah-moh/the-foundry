package bench

import (
	"fmt"
	"sort"
	"time"
)

// Observation holds one metric's value and how it was obtained.
type Observation struct {
	MetricID MetricID `json:"metric_id"`
	Value    *float64 `json:"value,omitempty"`
	Basis    Basis    `json:"basis"`
	Proxy    bool     `json:"proxy,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// GitProvenance captures git-derived timing for a control-arm delivery.
type GitProvenance struct {
	MergeRef       string    `json:"merge_ref"`
	BranchBase     string    `json:"branch_base"`
	BranchTip      string    `json:"branch_tip"`
	FirstCommitAt  time.Time `json:"first_commit_at"`
	FirstCommitSHA string    `json:"first_commit_sha"`
	MergedAt       time.Time `json:"merged_at"`
	FilesChanged   int       `json:"files_changed"`
}

// RunRecord is one durable measurement run (docs/PLAN.md Task 134 Step 3).
type RunRecord struct {
	ID                string         `json:"id"`
	Arm               Arm            `json:"arm"`
	WorkItemID        string         `json:"work_item_id"`
	WorkItemTitle     string         `json:"work_item_title"`
	RecordedAt        time.Time      `json:"recorded_at"`
	EnvironmentDigest string         `json:"environment_digest"`
	Observations      []Observation  `json:"observations"`
	Git               *GitProvenance `json:"git,omitempty"`
}

// HumanInput supplies the two control-arm fields git cannot see (B12).
type HumanInput struct {
	OrchestrationHours  float64 `yaml:"orchestration_hours"`
	ManualPromptsTouches int    `yaml:"manual_prompts_touches"`
	Reporter            string  `yaml:"reporter,omitempty"`
	ReportedAt          string  `yaml:"reported_at,omitempty"`
}

// NewRunRecord constructs a record with empty observations for every metric.
func NewRunRecord(id string, arm Arm, workItemID, title, envDigest string) *RunRecord {
	now := time.Now().UTC()
	obs := make([]Observation, 0, len(AllMetrics()))
	for _, d := range AllMetrics() {
		obs = append(obs, Observation{
			MetricID: d.ID,
			Basis:    BasisNotMeasurable,
		})
	}
	return &RunRecord{
		ID:                id,
		Arm:               arm,
		WorkItemID:        workItemID,
		WorkItemTitle:     title,
		RecordedAt:        now,
		EnvironmentDigest: envDigest,
		Observations:      obs,
	}
}

// SetObservation updates or inserts an observation for metricID.
func (r *RunRecord) SetObservation(metricID MetricID, value *float64, basis Basis, proxy bool, note string) error {
	if _, err := DefinitionByID(metricID); err != nil {
		return err
	}
	for i := range r.Observations {
		if r.Observations[i].MetricID == metricID {
			r.Observations[i].Value = value
			r.Observations[i].Basis = basis
			r.Observations[i].Proxy = proxy
			r.Observations[i].Note = note
			return nil
		}
	}
	r.Observations = append(r.Observations, Observation{
		MetricID: metricID,
		Value:    value,
		Basis:    basis,
		Proxy:    proxy,
		Note:     note,
	})
	sort.Slice(r.Observations, func(i, j int) bool {
		return r.Observations[i].MetricID < r.Observations[j].MetricID
	})
	return nil
}

// ObservationFor returns the observation for metricID.
func (r *RunRecord) ObservationFor(metricID MetricID) (Observation, bool) {
	for _, o := range r.Observations {
		if o.MetricID == metricID {
			return o, true
		}
	}
	return Observation{}, false
}

// Validate checks arm tag, digest, and that every known metric is present.
func (r *RunRecord) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("bench: record id is required")
	}
	switch r.Arm {
	case ArmControl, ArmFoundry:
	default:
		return fmt.Errorf("bench: invalid arm %q", r.Arm)
	}
	if r.EnvironmentDigest == "" {
		return fmt.Errorf("bench: environment digest is required")
	}
	seen := make(map[MetricID]struct{}, len(r.Observations))
	for _, o := range r.Observations {
		if _, err := DefinitionByID(o.MetricID); err != nil {
			return err
		}
		seen[o.MetricID] = struct{}{}
	}
	for _, d := range AllMetrics() {
		if _, ok := seen[d.ID]; !ok {
			return fmt.Errorf("bench: record %s missing metric %q", r.ID, d.ID)
		}
	}
	return nil
}

// ApplyHumanInput sets the two human-reported control-arm fields.
func (r *RunRecord) ApplyHumanInput(in HumanInput) error {
	hours := in.OrchestrationHours
	if err := r.SetObservation(MetricHumanOrchestration, &hours, BasisHumanReported, false, "operator-supplied orchestration log (B12)"); err != nil {
		return err
	}
	touches := float64(in.ManualPromptsTouches)
	return r.SetObservation(MetricManualPrompts, &touches, BasisHumanReported, false, "operator-supplied prompt/touch count (B12)")
}

// ApplyGitDelivery populates git-derived proxy timings on a control-arm record.
func (r *RunRecord) ApplyGitDelivery(g GitProvenance, proxyDefectCount float64, proxyDefectNote string) error {
	r.Git = &g
	hoursFirstToMerged := g.MergedAt.Sub(g.FirstCommitAt).Hours()
	if hoursFirstToMerged < 0 {
		return fmt.Errorf("bench: negative first-commit→merged duration for %s", r.ID)
	}
	basis := BasisInstrumented
	if r.Arm == ArmControl {
		basis = BasisProxy
	}
	note := "git: first branch commit → merge (PLAN proxy for control arm)"
	if err := r.SetObservation(MetricPlanToFirstAccepted, &hoursFirstToMerged, basis, r.Arm == ArmControl, note); err != nil {
		return err
	}
	if err := r.SetObservation(MetricPlanToVerified, &hoursFirstToMerged, basis, r.Arm == ArmControl, note+" (verified-completion proxy)"); err != nil {
		return err
	}
	return r.SetObservation(MetricDefectsAfterHandoff, &proxyDefectCount, BasisProxy, true, proxyDefectNote)
}
