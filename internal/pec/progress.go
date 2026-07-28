package pec

import (
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// PlanProgress is PEC's view of a plan's execution progress.
// Computed from the kernel's transition log; used for digest/reporting.
type PlanProgress struct {
	// TotalTasks is the count of tasks in the plan.
	TotalTasks int
	// CompletedTasks is the count of tasks whose terminal transition is SUCCEEDED.
	CompletedTasks int
	// FailedTasks is the count of tasks whose terminal transition is FAILED.
	FailedTasks int
	// Summary describes the progress in one sentence.
	Summary string
}

// TransitionSummary is one task's terminal status, extracted from the
// kernel's transition log for PEC's progress view.
type TransitionSummary struct {
	TaskID string
	Status state.Status
}

// ReportProgress computes a PlanProgress from a slice of TransitionSummaries.
// PEC never writes to the transitions table — it only reads summaries the
// kernel provides (Constitution C5: no side effects).
func ReportProgress(transitions []TransitionSummary) PlanProgress {
	p := PlanProgress{TotalTasks: len(transitions)}
	for _, t := range transitions {
		switch t.Status {
		case state.StatusSucceeded:
			p.CompletedTasks++
		case state.StatusFailed, state.StatusCancelled:
			p.FailedTasks++
		}
	}
	remaining := p.TotalTasks - p.CompletedTasks - p.FailedTasks
	if p.FailedTasks > 0 {
		p.Summary = fmt.Sprintf("%d/%d tasks completed (%d failed, %d remaining)", p.CompletedTasks, p.TotalTasks, p.FailedTasks, remaining)
	} else if remaining == 0 && p.CompletedTasks == p.TotalTasks {
		p.Summary = fmt.Sprintf("all %d tasks completed", p.TotalTasks)
	} else {
		p.Summary = fmt.Sprintf("%d/%d tasks completed (%d remaining)", p.CompletedTasks, p.TotalTasks, remaining)
	}
	return p
}
