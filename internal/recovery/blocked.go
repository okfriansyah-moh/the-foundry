// blocked.go: docs/PLAN.md Task 32 / FND-13's PROVEN_BLOCKED path
// (Constitution C22, docs/foundry/docs/workflows/recovery.md §20.11
// "Honest completion guarantee").
//
// Exhausting a retry budget (retrypolicy.go's ActionStop) is not, by
// itself, proof a task is impossible — it might just need a human retry
// with different inputs. PROVEN_BLOCKED is reserved for the narrower case
// where at least one recorded failure carries independently-established
// evidence that no retry could ever succeed. This package never infers
// that evidence itself (e.g. by pattern-matching FailureSignature.Detail
// for the word "dependency") — string-sniffing error text is a guess, not
// evidence, and Constitution C22 requires PROVEN_BLOCKED to carry real
// evidence refs. Instead, Evaluate composes a decision from
// FailureSignature.MissingDependency/ContradictorySpec flags a caller
// with actual domain knowledge (e.g. plan admission, or a dependency
// checker) has already set, together with the EvidenceRefs it attached.
package recovery

import (
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// ProvenBlocked is the terminal payload for a FAILED/PROVEN_BLOCKED
// outcome: state.ResultProvenBlocked plus the NextAction and evidence
// refs docs/PLAN.md Task 32's Acceptance requires ("PROVEN_BLOCKED
// carries evidence refs + next_action").
type ProvenBlocked struct {
	ResultCode   state.ResultCode
	Reason       string
	NextAction   string
	EvidenceRefs []string
}

// Evaluate inspects history (the same failure history retrypolicy.go's
// Policy.Decide was given) for the first signature carrying an
// impossibility flag, and if found, returns the ProvenBlocked payload for
// it. ok is false when no signature in history proves impossibility — the
// caller should then treat the ActionStop as a plain FAILED, not
// PROVEN_BLOCKED (docs/foundry/docs/workflows/recovery.md §20.11.1's
// non-stall guarantee still requires *some* terminal or waiting outcome
// either way; Evaluate only decides which terminal result_code applies).
//
// Evaluate itself calls Validate before ever returning ok=true, so
// Constitution C22's evidence guarantee is enforced at the one place a
// ProvenBlocked value is constructed, not left resting on every future
// caller remembering to call Validate separately. A signature that sets an
// impossibility flag but carries no EvidenceRefs therefore falls through as
// ok=false (plain FAILED), never a hollow PROVEN_BLOCKED.
func Evaluate(history []FailureSignature) (blocked ProvenBlocked, ok bool) {
	for _, sig := range history {
		var candidate ProvenBlocked
		switch {
		case sig.MissingDependency:
			candidate = ProvenBlocked{
				ResultCode:   state.ResultProvenBlocked,
				Reason:       "missing-dependency",
				NextAction:   "operator: resolve the missing dependency (see evidence refs) and resubmit the plan",
				EvidenceRefs: sig.EvidenceRefs,
			}
		case sig.ContradictorySpec:
			candidate = ProvenBlocked{
				ResultCode:   state.ResultProvenBlocked,
				Reason:       "contradictory-spec",
				NextAction:   "operator: the plan's acceptance criteria contradict each other (see evidence refs) — revise the spec and resubmit",
				EvidenceRefs: sig.EvidenceRefs,
			}
		default:
			continue
		}
		if err := candidate.Validate(); err != nil {
			continue
		}
		return candidate, true
	}
	return ProvenBlocked{}, false
}

// Validate reports an error if blocked violates Constitution C22's
// evidence requirement (a PROVEN_BLOCKED with no evidence refs proves
// nothing) or carries a ResultCode other than state.ResultProvenBlocked.
// Callers that build a state.Transition from blocked should call this
// first.
func (b ProvenBlocked) Validate() error {
	if b.ResultCode != state.ResultProvenBlocked {
		return fmt.Errorf("recovery: ProvenBlocked.ResultCode = %q, want %q", b.ResultCode, state.ResultProvenBlocked)
	}
	if len(b.EvidenceRefs) == 0 {
		return fmt.Errorf("recovery: PROVEN_BLOCKED requires at least one evidence ref")
	}
	if b.NextAction == "" {
		return fmt.Errorf("recovery: PROVEN_BLOCKED requires a next_action")
	}
	return nil
}
