package plan

import "fmt"

// RepoRef declares a repository a plan's tasks operate against.
type RepoRef struct {
	Alias  string `yaml:"alias"`
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
}

// Task is one unit of work within a Document.
type Task struct {
	ID                 string   `yaml:"id"`
	Goal               string   `yaml:"goal"`
	DependsOn          []string `yaml:"depends_on"`
	Commands           []string `yaml:"commands"`
	ValidationCommands []string `yaml:"validation_commands"`
	Files              []string `yaml:"files"`
}

// Section is one `## Heading` block of the Markdown body, preserved in
// document order. Sections carry human-readable plan narrative (rationale,
// notes) that sits alongside — but is not part of — the machine-checked
// schema fields.
type Section struct {
	Heading string
	Body    string
}

// Document is a parsed executable PLAN.md: YAML front matter fields plus the
// sectioned Markdown body that follows it.
type Document struct {
	ID                   string       `yaml:"id"`
	Title                string       `yaml:"title"`
	Version              string       `yaml:"version"`
	Repos                []RepoRef    `yaml:"repos"`
	Tasks                []Task       `yaml:"tasks"`
	DeclaredEffects      []Effect     `yaml:"declared_effects"`
	RequestedPermissions []Permission `yaml:"requested_permissions"`
	DeclaredTierIgnored  string       `yaml:"declared_tier"`
	BudgetUSD            float64      `yaml:"budget_usd"`

	// SelfClassified is true when the plan author declared a risk tier via
	// DeclaredTierIgnored. Task 7's AdmissionClassifier consumes this flag
	// to reject self-classifying plans (Constitution C6); this package does
	// not itself reject anything.
	SelfClassified bool `yaml:"-"`

	// Sections holds the parsed Markdown body, in document order.
	Sections []Section `yaml:"-"`

	// raw holds the original, unmodified source bytes as read, used by
	// Digest to compute the canonical content hash.
	raw []byte
}

// validate checks cross-field invariants that a plain YAML unmarshal cannot
// express: closed enums and referential integrity of DependsOn.
func (d *Document) validate() error {
	if d.ID == "" {
		return fmt.Errorf("plan: missing required field %q", "id")
	}
	if d.Version == "" {
		return fmt.Errorf("plan: missing required field %q", "version")
	}
	if len(d.Tasks) == 0 {
		return fmt.Errorf("plan: at least one task is required")
	}

	ids := make(map[string]struct{}, len(d.Tasks))
	for _, t := range d.Tasks {
		if t.ID == "" {
			return fmt.Errorf("plan: task missing required field %q", "id")
		}
		if _, dup := ids[t.ID]; dup {
			return fmt.Errorf("plan: duplicate task id %q", t.ID)
		}
		ids[t.ID] = struct{}{}
	}
	for _, t := range d.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("plan: task %q depends_on unknown task %q", t.ID, dep)
			}
		}
	}

	for i, e := range d.DeclaredEffects {
		if !e.Kind.Valid() {
			return fmt.Errorf("plan: declared_effects[%d] has unknown kind %q", i, e.Kind)
		}
	}

	if d.DeclaredTierIgnored != "" {
		d.SelfClassified = true
	}

	return nil
}
