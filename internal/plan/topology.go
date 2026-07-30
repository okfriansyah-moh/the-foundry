package plan

import (
	"fmt"
	"sort"
	"strings"
)

// TopologyTask is one node in a plan's task DAG for topology validation: its
// ID, the tasks it depends on, its declared parallel-wave membership (empty
// when the plan does not declare waves), and its normalized output paths.
type TopologyTask struct {
	ID        string
	DependsOn []string
	Wave      int // 0 means "unassigned / no declared wave"
	Files     []string
}

// ValidateTopology is a deterministic static validator over a plan's task DAG
// (docs/PLAN.md Task 110 / INT-02). It returns a sorted list of human-readable
// violation strings; an empty slice means the topology is sound. It enforces:
//
//   - no self-dependency;
//   - no dependency cycle;
//   - no unknown/dangling dependency reference;
//   - no task assigned before (or in the same wave as) a dependency;
//   - no direct or transitive dependency between two tasks in one parallel wave;
//   - no shared output path between two tasks in one parallel wave.
//
// Ambiguous path overlap inside a wave fails closed (reported) unless the plan
// serializes the offending tasks into different waves.
func ValidateTopology(tasks []TopologyTask) []string {
	var violations []string

	ids := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if ids[t.ID] {
			violations = append(violations, fmt.Sprintf("duplicate task id %q", t.ID))
		}
		ids[t.ID] = true
	}

	// Self-deps and unknown references.
	adj := map[string][]string{}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if dep == t.ID {
				violations = append(violations, fmt.Sprintf("task %q depends on itself", t.ID))
				continue
			}
			if !ids[dep] {
				violations = append(violations, fmt.Sprintf("task %q depends on unknown task %q", t.ID, dep))
				continue
			}
			adj[t.ID] = append(adj[t.ID], dep)
		}
	}

	// Cycle detection (DFS over the dependency edges).
	if cyc := findCycle(tasks, adj); cyc != "" {
		violations = append(violations, "dependency cycle detected: "+cyc)
	}

	// Transitive-reachability closure for wave checks.
	reach := transitiveReach(ids, adj)

	// Wave-aware checks (only when waves are declared, i.e. some Wave > 0).
	wavesDeclared := false
	byWave := map[int][]TopologyTask{}
	waveOf := map[string]int{}
	for _, t := range tasks {
		if t.Wave > 0 {
			wavesDeclared = true
		}
		byWave[t.Wave] = append(byWave[t.Wave], t)
		waveOf[t.ID] = t.Wave
	}

	if wavesDeclared {
		// A task must not be assigned before or in the same wave as a dependency.
		for _, t := range tasks {
			for _, dep := range adj[t.ID] {
				dw := waveOf[dep]
				if t.Wave <= dw {
					violations = append(violations, fmt.Sprintf(
						"task %q (wave %d) is assigned before or with its dependency %q (wave %d)", t.ID, t.Wave, dep, dw))
				}
			}
		}
		// Within a single wave: no dependency (direct or transitive) and no
		// shared output path between two members.
		waveNums := make([]int, 0, len(byWave))
		for w := range byWave {
			if w > 0 {
				waveNums = append(waveNums, w)
			}
		}
		sort.Ints(waveNums)
		for _, w := range waveNums {
			members := byWave[w]
			for i := 0; i < len(members); i++ {
				for j := i + 1; j < len(members); j++ {
					a, b := members[i], members[j]
					if reach[a.ID][b.ID] || reach[b.ID][a.ID] {
						violations = append(violations, fmt.Sprintf(
							"tasks %q and %q share parallel wave %d but one depends on the other", a.ID, b.ID, w))
					}
					if overlap := pathOverlap(a.Files, b.Files); overlap != "" {
						violations = append(violations, fmt.Sprintf(
							"tasks %q and %q in parallel wave %d overlap on output path %q", a.ID, b.ID, w, overlap))
					}
				}
			}
		}
	}

	sort.Strings(violations)
	return violations
}

// findCycle returns a readable cycle path if the dependency graph has one.
func findCycle(tasks []TopologyTask, adj map[string][]string) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var dfs func(string) string
	dfs = func(n string) string {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range adj[n] {
			switch color[m] {
			case gray:
				return strings.Join(append(stack, m), " -> ")
			case white:
				if c := dfs(m); c != "" {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return ""
	}
	// Deterministic order.
	order := make([]string, 0, len(tasks))
	for _, t := range tasks {
		order = append(order, t.ID)
	}
	sort.Strings(order)
	for _, id := range order {
		if color[id] == white {
			if c := dfs(id); c != "" {
				return c
			}
		}
	}
	return ""
}

// transitiveReach computes, for each task, the set of tasks reachable via
// dependency edges.
func transitiveReach(ids map[string]bool, adj map[string][]string) map[string]map[string]bool {
	reach := map[string]map[string]bool{}
	for id := range ids {
		seen := map[string]bool{}
		var walk func(string)
		walk = func(n string) {
			for _, m := range adj[n] {
				if !seen[m] {
					seen[m] = true
					walk(m)
				}
			}
		}
		walk(id)
		reach[id] = seen
	}
	return reach
}

// pathOverlap returns the first overlapping normalized path between two file
// sets, or "" when none. A shared prefix directory counts as an overlap (fail
// closed on ambiguity).
func pathOverlap(a, b []string) string {
	na := normalizePaths(a)
	nb := normalizePaths(b)
	for _, pa := range na {
		for _, pb := range nb {
			if pathsConflict(pa, pb) {
				return pa
			}
		}
	}
	return ""
}

func normalizePaths(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "./")
		p = strings.TrimSuffix(p, "/")
		if p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// pathsConflict reports whether two normalized paths overlap: identical, or one
// is a directory ancestor of the other (or a shared wildcard prefix).
func pathsConflict(a, b string) bool {
	if a == b {
		return true
	}
	as := strings.TrimSuffix(strings.TrimSuffix(a, "**"), "*")
	bs := strings.TrimSuffix(strings.TrimSuffix(b, "**"), "*")
	as = strings.TrimSuffix(as, "/")
	bs = strings.TrimSuffix(bs, "/")
	if as == "" || bs == "" {
		return false
	}
	return strings.HasPrefix(as+"/", bs+"/") || strings.HasPrefix(bs+"/", as+"/")
}

// TopologyFromDocument builds a dependency-only TopologyTask set from a parsed
// plan Document (no declared waves; files come from each task's Files).
func TopologyFromDocument(d *Document) []TopologyTask {
	out := make([]TopologyTask, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		out = append(out, TopologyTask{ID: t.ID, DependsOn: t.DependsOn, Files: t.Files})
	}
	return out
}
