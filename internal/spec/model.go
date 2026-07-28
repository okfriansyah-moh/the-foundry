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

// Specification is a post-pass-complete requirement set plus risk feed.
type Specification struct {
	Requirements       []Requirement    `json:"requirements" yaml:"requirements"`
	UnresolvedByImpact map[Impact]int   `json:"unresolved_by_impact" yaml:"unresolved_by_impact"`
	BySection          map[string][]int `json:"-" yaml:"-"`
	Sections           []string         `json:"-" yaml:"-"`
}
