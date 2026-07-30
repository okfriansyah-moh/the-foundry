package plan

import (
	"os"
	"strings"
	"testing"
)

func hasViolation(vs []string, substr string) bool {
	for _, v := range vs {
		if strings.Contains(v, substr) {
			return true
		}
	}
	return false
}

func TestValidateTopology_SoundGraphPasses(t *testing.T) {
	tasks := []TopologyTask{
		{ID: "a", Wave: 1, Files: []string{"src/a"}},
		{ID: "b", Wave: 1, Files: []string{"src/b"}},
		{ID: "c", DependsOn: []string{"a", "b"}, Wave: 2, Files: []string{"src/c"}},
	}
	if v := ValidateTopology(tasks); len(v) != 0 {
		t.Fatalf("sound graph must pass, got %v", v)
	}
}

func TestValidateTopology_EachRuleFires(t *testing.T) {
	cases := []struct {
		name   string
		tasks  []TopologyTask
		expect string
	}{
		{"self-dep", []TopologyTask{{ID: "a", DependsOn: []string{"a"}}}, "depends on itself"},
		{"cycle", []TopologyTask{{ID: "a", DependsOn: []string{"b"}}, {ID: "b", DependsOn: []string{"a"}}}, "cycle detected"},
		{"unknown", []TopologyTask{{ID: "a", DependsOn: []string{"ghost"}}}, "unknown task"},
		{"before-dep", []TopologyTask{{ID: "a", Wave: 2}, {ID: "b", DependsOn: []string{"a"}, Wave: 1}}, "assigned before or with its dependency"},
		{"transitive-in-wave", []TopologyTask{{ID: "a", Wave: 1}, {ID: "b", DependsOn: []string{"a"}, Wave: 1}}, "one depends on the other"},
		{"path-overlap", []TopologyTask{{ID: "a", Wave: 1, Files: []string{"src/shared"}}, {ID: "b", Wave: 1, Files: []string{"src/shared"}}}, "overlap on output path"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			v := ValidateTopology(tc.tasks)
			if !hasViolation(v, tc.expect) {
				t.Fatalf("expected a violation containing %q, got %v", tc.expect, v)
			}
		})
	}
}

func TestValidateTopology_TransitiveDependencyInWave(t *testing.T) {
	// a <- b <- c, with a and c in the same wave: c transitively depends on a.
	tasks := []TopologyTask{
		{ID: "a", Wave: 1},
		{ID: "b", DependsOn: []string{"a"}, Wave: 2},
		{ID: "c", DependsOn: []string{"b"}, Wave: 1},
	}
	v := ValidateTopology(tasks)
	if !hasViolation(v, "one depends on the other") {
		t.Fatalf("transitive intra-wave dependency must be caught, got %v", v)
	}
}

// TestTopologySeedsFail asserts the deliberately-broken plan_topology fitness
// seeds are actually caught by the validator (docs/PLAN.md Task 110).
func TestTopologySeedsFail(t *testing.T) {
	seeds := map[string]string{
		"../../test/fitness_seeds/plan_topology/self-dependency.md": "depends on itself",
		"../../test/fitness_seeds/plan_topology/cycle.md":           "cycle detected",
	}
	for path, want := range seeds {
		raw, err := readFile(t, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		doc, err := ParseBytes(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if v := ValidateTopology(TopologyFromDocument(doc)); !hasViolation(v, want) {
			t.Fatalf("seed %s should fail with %q, got %v", path, want, v)
		}
	}
}

func readFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(path)
}
