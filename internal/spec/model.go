package spec

// Label is the mandatory provenance label for every synthesized statement.
type Label string

const (
	LabelObserved   Label = "Observed"
	LabelInferred   Label = "Inferred"
	LabelAssumed    Label = "Assumed"
	LabelUnresolved Label = "Unresolved"
)

func (l Label) Valid() bool {
	switch l {
	case LabelObserved, LabelInferred, LabelAssumed, LabelUnresolved:
		return true
	default:
		return false
	}
}

// Impact is a coarse risk impact tier for unresolved items; Task 45 consumes
// unresolved counts by impact.
type Impact string

const (
	ImpactLow    Impact = "low"
	ImpactMedium Impact = "medium"
	ImpactHigh   Impact = "high"
)

func (i Impact) Valid() bool {
	switch i {
	case ImpactLow, ImpactMedium, ImpactHigh:
		return true
	default:
		return false
	}
}

// Requirement is one labeled requirement statement.
type Requirement struct {
	ID      string `json:"id" yaml:"id"`
	Section string `json:"section" yaml:"section"`
	Text    string `json:"text" yaml:"text"`
	Label   Label  `json:"label" yaml:"label"`
	Basis   string `json:"basis" yaml:"basis"`
	Impact  Impact `json:"impact" yaml:"impact"`
}

// SpecProvenance records what produced a Specification when it was synthesized
// by an LLM (docs/PLAN.md Task 109 / INT-01): the provider, model and a digest
// of the exact prompt. This is provenance, not authorization — labels, bases
// and completeness remain decided by PostPass (Constitution C16).
type SpecProvenance struct {
	Provider     string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model        string `json:"model,omitempty" yaml:"model,omitempty"`
	PromptDigest string `json:"prompt_digest,omitempty" yaml:"prompt_digest,omitempty"`
}

// Specification is a post-pass-complete requirement set plus risk feed.
type Specification struct {
	Requirements       []Requirement    `json:"requirements" yaml:"requirements"`
	UnresolvedByImpact map[Impact]int   `json:"unresolved_by_impact" yaml:"unresolved_by_impact"`
	Provenance         SpecProvenance   `json:"provenance,omitempty" yaml:"provenance,omitempty"`
	BySection          map[string][]int `json:"-" yaml:"-"`
	Sections           []string         `json:"-" yaml:"-"`
}
