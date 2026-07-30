package spec

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EffectMappingRow struct {
	Section string `yaml:"section"`
	Kind    string `yaml:"kind"`
	Target  string `yaml:"target"`
}

type EffectMapping struct {
	Rows []EffectMappingRow `yaml:"rows"`
}

type generatedEffect struct {
	Kind   string `yaml:"kind"`
	Target string `yaml:"target"`
}

type generatedTask struct {
	ID                     string   `yaml:"id"`
	Goal                   string   `yaml:"goal"`
	Commands               []string `yaml:"commands,omitempty"`
	ValidationCommands     []string `yaml:"validation_commands,omitempty"`
	ValidationOptOut       bool     `yaml:"validation_optout,omitempty"`
	ValidationOptOutReason string   `yaml:"validation_optout_reason,omitempty"`
	Files                  []string `yaml:"files"`
}

type generatedPlan struct {
	ID                   string            `yaml:"id"`
	Title                string            `yaml:"title"`
	Version              string            `yaml:"version"`
	Repos                []map[string]any  `yaml:"repos"`
	Tasks                []generatedTask   `yaml:"tasks"`
	DeclaredEffects      []generatedEffect `yaml:"declared_effects"`
	RequestedPermissions []generatedEffect `yaml:"requested_permissions"`
	BudgetUSD            float64           `yaml:"budget_usd"`
}

// MissionContext supplies the real, mission-owned values a generated plan must
// use instead of literals: the repository it operates on, its budget envelope,
// and the specific repo-write path least-privilege permissions target
// (docs/PLAN.md Task 110 / INT-02). RepoWriteTarget is never "*".
type MissionContext struct {
	RepoAlias       string
	RepoURL         string
	RepoBranch      string
	BudgetUSD       float64
	RepoWriteTarget string
}

// noCommandValidationReason is the Task 104 opt-out reason a generated task
// records when its validation cannot be expressed as an allowlisted command
// from requirement text alone.
const noCommandValidationReason = "generated from requirement text with no command-expressible validation; requires human-recorded verification (Task 104 opt-out)"

// PlanFromSpecification produces an executable, least-privilege PLAN from a
// specification (docs/PLAN.md Task 110 / INT-02): tasks derived from
// requirement clusters carrying their requirement IDs, real per-task validation
// (an allowlisted command or the explicit Task-104 opt-out -- never a hollow
// make test), the mission's actual repository and budget, and only the
// permissions the tasks need (repo-write targets the mission repo path, never
// "*"). It never self-classifies (Constitution C6).
func PlanFromSpecification(planID, title string, s Specification, mapping EffectMapping, mc MissionContext) ([]byte, error) {
	if strings.TrimSpace(planID) == "" {
		return nil, fmt.Errorf("spec plangen: plan id is required")
	}
	if strings.TrimSpace(mc.RepoURL) == "" {
		return nil, fmt.Errorf("spec plangen: mission repository url is required (no literal fallback)")
	}
	if t := strings.TrimSpace(mc.RepoWriteTarget); t == "" || t == "*" {
		return nil, fmt.Errorf("spec plangen: repo-write target must be a specific mission path, never empty or a wildcard")
	}
	sections := orderedSections(s.Sections)
	if len(sections) == 0 {
		return nil, fmt.Errorf("spec plangen: empty specification")
	}

	tasks := make([]generatedTask, 0, len(sections))
	for i, section := range sections {
		reqIDs := requirementIDsForSection(s, section)
		goal := fmt.Sprintf("Implement %s requirements [%s]", section, strings.Join(reqIDs, ", "))
		tasks = append(tasks, generatedTask{
			ID:                     fmt.Sprintf("task-%02d-%s", i+1, strings.ReplaceAll(section, " ", "-")),
			Goal:                   goal,
			ValidationOptOut:       true,
			ValidationOptOutReason: noCommandValidationReason,
			Files:                  []string{"src/" + strings.ReplaceAll(section, " ", "-")},
		})
	}

	effects := map[string]generatedEffect{}
	for _, r := range mapping.Rows {
		for _, section := range sections {
			if section != r.Section {
				continue
			}
			key := r.Kind + "|" + r.Target
			effects[key] = generatedEffect{Kind: r.Kind, Target: r.Target}
		}
	}
	declared := make([]generatedEffect, 0, len(effects))
	for _, ef := range effects {
		declared = append(declared, ef)
	}
	sort.SliceStable(declared, func(i, j int) bool {
		if declared[i].Kind == declared[j].Kind {
			return declared[i].Target < declared[j].Target
		}
		return declared[i].Kind < declared[j].Kind
	})

	permSet := map[string]generatedEffect{}
	permSet["repo-write|"+mc.RepoWriteTarget] = generatedEffect{Kind: "repo-write", Target: mc.RepoWriteTarget}
	for _, ef := range declared {
		if ef.Target == "*" {
			return nil, fmt.Errorf("spec plangen: declared effect %q has wildcard target -- refuse to widen a permission", ef.Kind)
		}
		permSet[ef.Kind+"|"+ef.Target] = generatedEffect{Kind: ef.Kind, Target: ef.Target}
	}
	permissions := make([]generatedEffect, 0, len(permSet))
	for _, p := range permSet {
		if p.Target == "*" {
			return nil, fmt.Errorf("spec plangen: refuse to emit a wildcard permission target")
		}
		permissions = append(permissions, p)
	}
	sort.SliceStable(permissions, func(i, j int) bool {
		if permissions[i].Kind == permissions[j].Kind {
			return permissions[i].Target < permissions[j].Target
		}
		return permissions[i].Kind < permissions[j].Kind
	})

	branch := mc.RepoBranch
	if branch == "" {
		branch = "main"
	}
	alias := mc.RepoAlias
	if alias == "" {
		alias = "product"
	}

	doc := generatedPlan{
		ID:      planID,
		Title:   title,
		Version: "1.0",
		Repos: []map[string]any{
			{"alias": alias, "url": mc.RepoURL, "branch": branch},
		},
		Tasks:                tasks,
		DeclaredEffects:      declared,
		RequestedPermissions: permissions,
		BudgetUSD:            mc.BudgetUSD,
	}

	frontMatter, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("spec plangen: marshal front matter: %w", err)
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(frontMatter)
	out.WriteString("---\n")
	out.WriteString("## Rationale\n\nGenerated from specification requirement clusters with deterministic, least-privilege effect mapping. Each task is traceable to the requirement IDs in its goal.\n")
	return out.Bytes(), nil
}

// requirementIDsForSection returns the sorted requirement IDs a section's task
// satisfies, so the generated plan is traceable back to the spec.
func requirementIDsForSection(s Specification, section string) []string {
	idxs := s.BySection[section]
	ids := make([]string, 0, len(idxs))
	for _, i := range idxs {
		if i >= 0 && i < len(s.Requirements) {
			ids = append(ids, s.Requirements[i].ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		ids = []string{"req-" + section}
	}
	return ids
}
