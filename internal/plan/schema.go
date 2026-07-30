package plan

import (
	"fmt"
	"strings"
)

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
	// ValidationOptOut is the single, explicit, auditable escape for a task
	// that genuinely cannot be validated by command (docs/PLAN.md Task 104 /
	// SKP-11R2). A task with neither validation_commands nor this opt-out is a
	// plan-validation error — the honest-completion enforcement point may not
	// be bypassed by omission (Constitution C10). An opt-out task is a valid
	// plan task, but it can never auto-succeed: it requires a human-recorded
	// reason (ValidationOptOutReason) and downstream human sign-off.
	ValidationOptOut       bool   `yaml:"validation_optout"`
	ValidationOptOutReason string `yaml:"validation_optout_reason"`
	// Executor, when set, names the executor adapter this task must run on
	// (docs/PLAN.md Task 85 / PRV-02). Empty means "let the kernel choose"
	// (routing or the configured default). It is never authoritative on its
	// own: the kernel's ExecutorSelector still validates it against the
	// policy allowlist and capability registry and fails closed if denied.
	Executor string `yaml:"executor"`
	// Class, when set, is the task-class label (e.g. "architecture",
	// "frontend", "review") the kernel's routing table maps to an ordered
	// executor preference list (docs/PLAN.md Task 90 / PRV-07). Empty means
	// "unclassed" — an unclassed task with no explicit Executor uses the
	// configured default. A task that DOES declare a class, when routing is
	// active, must resolve to an eligible executor or selection fails closed
	// (it never silently defaults). Purely a routing input; PEC may propose
	// the label, but the kernel — never an LLM — makes the selection.
	Class string `yaml:"class"`
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

		// Honest-completion: every task must declare at least one validation
		// command, or take the explicit, reasoned opt-out (docs/PLAN.md Task
		// 104 / SKP-11R2, Constitution C10).
		if len(t.ValidationCommands) == 0 {
			if !t.ValidationOptOut {
				return fmt.Errorf("plan: task %q declares no validation_commands and no validation_optout (honest completion requires one or the other)", t.ID)
			}
			if strings.TrimSpace(t.ValidationOptOutReason) == "" {
				return fmt.Errorf("plan: task %q sets validation_optout but records no validation_optout_reason", t.ID)
			}
		}
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
