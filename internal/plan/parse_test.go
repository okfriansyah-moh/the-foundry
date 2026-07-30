package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "plans", name))
	if err != nil {
		t.Fatalf("read example %s: %v", name, err)
	}
	return raw
}

func TestParseExamples(t *testing.T) {
	cases := []struct {
		name          string
		file          string
		wantID        string
		wantTaskCount int
	}{
		{"hello-world", "hello-world.md", "plan-hello-world", 1},
		{"two-task", "two-task.md", "plan-two-task", 2},
		{"failing-task", "failing-task.md", "plan-failing-task", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseBytes(readExample(t, tc.file))
			if err != nil {
				t.Fatalf("ParseBytes: %v", err)
			}
			if doc.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", doc.ID, tc.wantID)
			}
			if len(doc.Tasks) != tc.wantTaskCount {
				t.Errorf("len(Tasks) = %d, want %d", len(doc.Tasks), tc.wantTaskCount)
			}
			if doc.SelfClassified {
				t.Errorf("SelfClassified = true, want false (no declared_tier in fixture)")
			}
			if len(doc.Sections) == 0 {
				t.Errorf("Sections = empty, want at least the Rationale section")
			}
		})
	}
}

func TestParseTwoTaskDependency(t *testing.T) {
	doc, err := ParseBytes(readExample(t, "two-task.md"))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if len(doc.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d, want 2", len(doc.Tasks))
	}
	if got := doc.Tasks[1].DependsOn; len(got) != 1 || got[0] != "t1" {
		t.Errorf("Tasks[1].DependsOn = %v, want [t1]", got)
	}
}

func TestParseSelfClassifiedFlag(t *testing.T) {
	const src = `---
id: plan-self-classified
version: "1.0"
tasks:
  - id: t1
    goal: attempt to self-classify
    validation_commands:
      - echo ok
declared_tier: A0
---
`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if !doc.SelfClassified {
		t.Errorf("SelfClassified = false, want true when declared_tier is set")
	}
	if doc.DeclaredTierIgnored != "A0" {
		t.Errorf("DeclaredTierIgnored = %q, want %q", doc.DeclaredTierIgnored, "A0")
	}
}

func TestParseStrictModeRejectsUnknownField(t *testing.T) {
	const src = `---
id: plan-unknown-field
version: "1.0"
tasks:
  - id: t1
    goal: has an unknown top-level field
totally_unknown_field: true
---
`
	_, err := ParseBytes([]byte(src))
	if err == nil {
		t.Fatal("ParseBytes: expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("error %q does not contain a line number", err.Error())
	}
}

func TestParseStrictModeRejectsUnknownTaskField(t *testing.T) {
	const src = `---
id: plan-unknown-task-field
version: "1.0"
tasks:
  - id: t1
    goal: has an unknown task field
    made_up_field: nope
---
`
	_, err := ParseBytes([]byte(src))
	if err == nil {
		t.Fatal("ParseBytes: expected error for unknown task field, got nil")
	}
}

func TestParseMissingFrontMatterDelimiter(t *testing.T) {
	_, err := ParseBytes([]byte("id: plan-no-delim\n"))
	if err == nil {
		t.Fatal("ParseBytes: expected error for missing front-matter delimiter, got nil")
	}
}

func TestParseUnterminatedFrontMatter(t *testing.T) {
	_, err := ParseBytes([]byte("---\nid: plan-unterminated\n"))
	if err == nil {
		t.Fatal("ParseBytes: expected error for unterminated front matter, got nil")
	}
}

func TestParseRejectsMissingRequiredFields(t *testing.T) {
	cases := map[string]string{
		"missing id":      "---\nversion: \"1.0\"\ntasks:\n  - id: t1\n    goal: g\n---\n",
		"missing version": "---\nid: plan-x\ntasks:\n  - id: t1\n    goal: g\n---\n",
		"no tasks":        "---\nid: plan-x\nversion: \"1.0\"\n---\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBytes([]byte(src)); err == nil {
				t.Fatalf("ParseBytes: expected error for %s, got nil", name)
			}
		})
	}
}

func TestParseRejectsDuplicateTaskID(t *testing.T) {
	const src = `---
id: plan-dup
version: "1.0"
tasks:
  - id: t1
    goal: first
  - id: t1
    goal: duplicate
---
`
	if _, err := ParseBytes([]byte(src)); err == nil {
		t.Fatal("ParseBytes: expected error for duplicate task id, got nil")
	}
}

func TestParseRejectsUnknownDependsOn(t *testing.T) {
	const src = `---
id: plan-baddep
version: "1.0"
tasks:
  - id: t1
    goal: depends on a task that does not exist
    depends_on:
      - does-not-exist
---
`
	if _, err := ParseBytes([]byte(src)); err == nil {
		t.Fatal("ParseBytes: expected error for unknown depends_on, got nil")
	}
}

func TestParseRejectsUnknownEffectKind(t *testing.T) {
	const src = `---
id: plan-badeffect
version: "1.0"
tasks:
  - id: t1
    goal: declares a bogus effect kind
declared_effects:
  - kind: teleportation
    target: somewhere
---
`
	if _, err := ParseBytes([]byte(src)); err == nil {
		t.Fatal("ParseBytes: expected error for unknown effect kind, got nil")
	}
}

func TestParseSectionsPreserveOrder(t *testing.T) {
	const src = `---
id: plan-sections
version: "1.0"
tasks:
  - id: t1
    goal: g
    validation_commands:
      - echo ok
---
## First

first body

## Second

second body
`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("len(Sections) = %d, want 2", len(doc.Sections))
	}
	if doc.Sections[0].Heading != "First" || doc.Sections[0].Body != "first body" {
		t.Errorf("Sections[0] = %+v", doc.Sections[0])
	}
	if doc.Sections[1].Heading != "Second" || doc.Sections[1].Body != "second body" {
		t.Errorf("Sections[1] = %+v", doc.Sections[1])
	}
}
