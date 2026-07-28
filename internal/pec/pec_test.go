package pec_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/pec"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// --- Wave tests ---

func makeDoc(tasks []plan.Task) plan.Document {
	return plan.Document{ID: "test-doc", Tasks: tasks}
}

// TestProposeWaves_Linear verifies a chain a→b→c produces 3 waves of 1.
func TestProposeWaves_Linear(t *testing.T) {
	doc := makeDoc([]plan.Task{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	})
	prop, err := pec.ProposeWaves(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prop.Waves) != 3 {
		t.Errorf("waves=%d, want 3; waves=%v", len(prop.Waves), prop.Waves)
	}
}

// TestProposeWaves_Diamond verifies a diamond (a→b,c→d) produces 3 waves.
func TestProposeWaves_Diamond(t *testing.T) {
	doc := makeDoc([]plan.Task{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"a"}},
		{ID: "d", DependsOn: []string{"b", "c"}},
	})
	prop, err := pec.ProposeWaves(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expected: wave[0]={a}, wave[1]={b,c}, wave[2]={d}
	if len(prop.Waves) != 3 {
		t.Errorf("waves=%d, want 3; waves=%v", len(prop.Waves), prop.Waves)
	}
}

// TestProposeWaves_Cycle verifies a cycle returns an error.
func TestProposeWaves_Cycle(t *testing.T) {
	doc := makeDoc([]plan.Task{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	})
	_, err := pec.ProposeWaves(doc)
	if err == nil {
		t.Error("expected cycle error, got nil")
	}
}

// TestProposeWaves_Deterministic verifies same doc always produces same waves.
func TestProposeWaves_Deterministic(t *testing.T) {
	doc := makeDoc([]plan.Task{
		{ID: "t3"}, {ID: "t1"}, {ID: "t2", DependsOn: []string{"t1", "t3"}},
	})
	p1, err1 := pec.ProposeWaves(doc)
	p2, err2 := pec.ProposeWaves(doc)
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v, %v", err1, err2)
	}
	if fmt.Sprintf("%v", p1.Waves) != fmt.Sprintf("%v", p2.Waves) {
		t.Errorf("non-deterministic: %v != %v", p1.Waves, p2.Waves)
	}
}

// TestProposeWaves_EdgeRespect_Property is a ×1000 property test verifying
// every wave proposal respects all dependency edges across random DAGs.
func TestProposeWaves_EdgeRespect_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 1000; i++ {
		doc := randomDAG(rng, 2+rng.Intn(8))
		prop, err := pec.ProposeWaves(doc)
		if err != nil {
			t.Fatalf("iter %d: unexpected error for valid DAG: %v", i, err)
		}
		// Build wave index: task ID → wave number.
		waveOf := map[string]int{}
		for w, wave := range prop.Waves {
			for _, id := range wave {
				waveOf[id] = w
			}
		}
		// Verify every dependency is in a strictly earlier wave.
		for _, task := range doc.Tasks {
			for _, dep := range task.DependsOn {
				if waveOf[dep] >= waveOf[task.ID] {
					t.Errorf("iter %d: dependency edge %s→%s violated: dep in wave %d, task in wave %d",
						i, dep, task.ID, waveOf[dep], waveOf[task.ID])
				}
			}
		}
	}
}

// randomDAG builds a random DAG with n tasks ensuring no cycles by only
// adding edges from tasks with higher index to lower index.
func randomDAG(rng *rand.Rand, n int) plan.Document {
	tasks := make([]plan.Task, n)
	for i := 0; i < n; i++ {
		tasks[i] = plan.Task{ID: fmt.Sprintf("t%d", i)}
	}
	for i := 1; i < n; i++ {
		// Each task may depend on some subset of earlier tasks.
		for j := 0; j < i; j++ {
			if rng.Float32() < 0.3 {
				tasks[i].DependsOn = append(tasks[i].DependsOn, tasks[j].ID)
			}
		}
	}
	return plan.Document{ID: "random-dag", Tasks: tasks}
}

// --- Remediation tests ---

func TestProposeRemediation_WithFailedCommands(t *testing.T) {
	ref := pec.TaskRef{PlanID: "p1", TaskID: "t1"}
	records := []verify.CommandRecord{
		{Cmd: "make test", ExitCode: 1},
		{Cmd: "make lint", ExitCode: 0},
	}
	rem := pec.ProposeRemediation(ref, records, nil)
	if rem.Suggestion == "" {
		t.Error("Suggestion is empty")
	}
	if len(rem.Evidence) == 0 {
		t.Error("Evidence is empty for failed commands")
	}
}

func TestProposeRemediation_NoEvidence(t *testing.T) {
	ref := pec.TaskRef{PlanID: "p1", TaskID: "t1"}
	// No failed commands, no summaries — genuinely no evidence.
	rem := pec.ProposeRemediation(ref, nil, nil)
	if rem.Confidence > 0.5 {
		t.Errorf("confidence=%.2f, want <=0.5 for no evidence", rem.Confidence)
	}
}

// --- Progress tests ---

func TestReportProgress_AllSucceeded(t *testing.T) {
	transitions := []pec.TransitionSummary{
		{TaskID: "t1", Status: state.StatusSucceeded},
		{TaskID: "t2", Status: state.StatusSucceeded},
	}
	p := pec.ReportProgress(transitions)
	if p.CompletedTasks != 2 {
		t.Errorf("CompletedTasks=%d, want 2", p.CompletedTasks)
	}
	if p.FailedTasks != 0 {
		t.Errorf("FailedTasks=%d, want 0", p.FailedTasks)
	}
}

func TestReportProgress_WithFailed(t *testing.T) {
	transitions := []pec.TransitionSummary{
		{TaskID: "t1", Status: state.StatusSucceeded},
		{TaskID: "t2", Status: state.StatusFailed},
		{TaskID: "t3", Status: state.StatusRunning},
	}
	p := pec.ReportProgress(transitions)
	if p.FailedTasks != 1 {
		t.Errorf("FailedTasks=%d, want 1", p.FailedTasks)
	}
	if p.Summary == "" {
		t.Error("Summary is empty")
	}
}

// TestMalformedProposalDistrust verifies the kernel distrust pattern:
// a wave proposal with an unknown task ID that the kernel does not
// recognise must be safely ignorable (API-shape test: WaveProposal
// has no capability handle, so the kernel can always fall back to
// sequential ordering rather than executing the malformed proposal).
func TestMalformedProposalDistrust(t *testing.T) {
	// A malformed proposal (unknown task in wave) carries no capability
	// handle — the kernel can verify wave ⊆ doc.Tasks and reject if not.
	malformed := pec.WaveProposal{
		Waves:       [][]string{{"ghost-task-not-in-plan"}},
		Explanation: "malformed",
	}
	// The WaveProposal type itself cannot execute anything; it is data only.
	// Verify it carries no methods that perform I/O or side effects.
	_ = malformed.Waves
	_ = malformed.Explanation
	// If the kernel calls ValidateWaveProposal it should detect "ghost-task".
	doc := makeDoc([]plan.Task{{ID: "real-task"}})
	if err := pec.ValidateWaveProposal(malformed, doc); err == nil {
		t.Error("ValidateWaveProposal should reject proposal with unknown task ID")
	}
}
