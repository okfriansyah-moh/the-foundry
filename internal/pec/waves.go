package pec

import (
	"fmt"
	"sort"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// WaveProposal is PEC's proposed wave ordering for a plan document.
// It is a proposal only — the kernel validates it against its own
// dependency check before use. If the proposal is malformed (e.g. cycle),
// the kernel falls back to sequential ordering.
type WaveProposal struct {
	// Waves is the ordered list of waves; each wave is a set of task IDs
	// that PEC believes can run concurrently (all dependencies of each
	// task ID are in a previous wave).
	Waves [][]string
	// Explanation is a human-readable note about the wave partition.
	Explanation string
}

// ValidateWaveProposal checks that every task ID in proposal.Waves appears
// in doc.Tasks. The kernel calls this before trusting any PEC proposal.
// If any wave contains an unknown task ID, it returns an error naming it.
func ValidateWaveProposal(proposal WaveProposal, doc plan.Document) error {
	known := make(map[string]struct{}, len(doc.Tasks))
	for _, t := range doc.Tasks {
		known[t.ID] = struct{}{}
	}
	for _, wave := range proposal.Waves {
		for _, id := range wave {
			if _, ok := known[id]; !ok {
				return fmt.Errorf("pec: wave proposal contains unknown task %q", id)
			}
		}
	}
	return nil
}

// Algorithm:
//  1. Topological sort via Kahn's algorithm on the DependsOn DAG.
//  2. Assign each task to the earliest wave where all its dependencies
//     have been placed in a prior wave.
//  3. Within a wave, sort by task ID for determinism.
//
// Returns an error if a cycle is detected.
func ProposeWaves(doc plan.Document) (WaveProposal, error) {
	// Build adjacency: task ID → set of its dependencies.
	deps := make(map[string]map[string]struct{}, len(doc.Tasks))
	inDegree := make(map[string]int, len(doc.Tasks))
	taskSet := make(map[string]struct{}, len(doc.Tasks))

	for _, t := range doc.Tasks {
		taskSet[t.ID] = struct{}{}
		if _, ok := deps[t.ID]; !ok {
			deps[t.ID] = make(map[string]struct{})
		}
		inDegree[t.ID] = 0
	}
	for _, t := range doc.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := taskSet[dep]; !ok {
				return WaveProposal{}, fmt.Errorf("pec waves: task %q depends on unknown task %q", t.ID, dep)
			}
			deps[t.ID][dep] = struct{}{}
		}
	}
	// Compute in-degree (number of not-yet-placed dependencies).
	for _, t := range doc.Tasks {
		inDegree[t.ID] = len(deps[t.ID])
	}

	// wave assignment: BFS-style Kahn layering.
	remaining := make(map[string]struct{}, len(doc.Tasks))
	for _, t := range doc.Tasks {
		remaining[t.ID] = struct{}{}
	}

	var waves [][]string
	for len(remaining) > 0 {
		// Collect all tasks with zero in-degree.
		var wave []string
		for id := range remaining {
			if inDegree[id] == 0 {
				wave = append(wave, id)
			}
		}
		if len(wave) == 0 {
			// Remaining tasks all have non-zero in-degree → cycle.
			remaining_ids := make([]string, 0, len(remaining))
			for id := range remaining {
				remaining_ids = append(remaining_ids, id)
			}
			sort.Strings(remaining_ids)
			return WaveProposal{}, fmt.Errorf("pec waves: dependency cycle detected among tasks %v", remaining_ids)
		}
		sort.Strings(wave) // deterministic tie-break by task ID
		waves = append(waves, wave)

		// Remove placed tasks and decrement in-degree of their dependents.
		for _, id := range wave {
			delete(remaining, id)
		}
		// Decrement in-degrees: for each placed task, find tasks that depend on it.
		for id := range remaining {
			for _, placed := range wave {
				if _, ok := deps[id][placed]; ok {
					inDegree[id]--
					delete(deps[id], placed)
				}
			}
		}
	}

	return WaveProposal{
		Waves:       waves,
		Explanation: fmt.Sprintf("%d task(s) in %d wave(s)", len(doc.Tasks), len(waves)),
	}, nil
}
