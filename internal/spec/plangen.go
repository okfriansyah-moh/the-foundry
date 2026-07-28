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
	ID                 string   `yaml:"id"`
	Goal               string   `yaml:"goal"`
	Commands           []string `yaml:"commands"`
	ValidationCommands []string `yaml:"validation_commands"`
	Files              []string `yaml:"files"`
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

func PlanFromSpecification(planID, title string, s Specification, mapping EffectMapping) ([]byte, error) {
	if strings.TrimSpace(planID) == "" {
		return nil, fmt.Errorf("spec plangen: plan id is required")
	}
	sections := orderedSections(s.Sections)
	if len(sections) == 0 {
		return nil, fmt.Errorf("spec plangen: empty specification")
	}
	tasks := make([]generatedTask, 0, len(sections))
	for i, section := range sections {
		tasks = append(tasks, generatedTask{
			ID:   fmt.Sprintf("task-%02d-%s", i+1, strings.ReplaceAll(section, " ", "-")),
			Goal: "Implement " + section + " requirements",
			Commands: []string{
				"make test",
			},
			ValidationCommands: []string{
				"make test",
			},
			Files: []string{
				"src/" + strings.ReplaceAll(section, " ", "-"),
			},
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

	doc := generatedPlan{
		ID:      planID,
		Title:   title,
		Version: "1.0",
		Repos: []map[string]any{
			{
				"alias":  "product",
				"url":    "https://github.com/example/generated-product",
				"branch": "main",
			},
		},
		Tasks:           tasks,
		DeclaredEffects: declared,
		RequestedPermissions: []generatedEffect{
			{Kind: "repo-write", Target: "*"},
		},
		BudgetUSD: 50,
	}

	frontMatter, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("spec plangen: marshal front matter: %w", err)
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(frontMatter)
	out.WriteString("---\n")
	out.WriteString("## Rationale\n\nGenerated from specification sections with deterministic effect mapping.\n")
	return out.Bytes(), nil
}
