package pec

import (
	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// TaskRef identifies one task in the context of a plan execution.
type TaskRef struct {
	PlanID string
	TaskID string
}

// Remediation is PEC's remediation proposal for a failed task.
// It is a suggestion only — the kernel decides whether to act on it.
// Proposal types carry NO capability handles (Constitution C5).
type Remediation struct {
	Suggestion string
	Confidence float64  // 0.0–1.0
	Evidence   []string // supporting command records and executor notes
}

// ProposeRemediation produces a remediation suggestion for a failed task.
// In production, this can call an LLM (cassette-driven in tests).
// The returned Remediation is advisory only — no kernel action is taken.
func ProposeRemediation(failed TaskRef, records []verify.CommandRecord, summaries []executor.Summary) Remediation {
	var evidence []string

	for _, r := range records {
		if r.ExitCode != 0 {
			evidence = append(evidence, "failed command: "+r.Cmd)
		}
		if r.PolicyViolation {
			evidence = append(evidence, "policy violation in: "+r.Cmd)
		}
	}
	for _, s := range summaries {
		if s.Claimed != "" {
			evidence = append(evidence, "executor claimed: "+s.Claimed)
		}
	}

	if len(evidence) == 0 {
		return Remediation{
			Suggestion: "no evidence of failure found; re-run validation",
			Confidence: 0.1,
		}
	}

	return Remediation{
		Suggestion: "review failed commands and re-run validation; if a policy violation, update the validation allowlist",
		Confidence: 0.6,
		Evidence:   evidence,
	}
}
