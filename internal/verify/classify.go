package verify

// Classification is the deterministic failure category a validation run is
// assigned, per docs/PLAN.md Task 13 Step 4. An empty Classification means
// every command succeeded.
type Classification string

// Classification values. These strings are the failure-classification
// vocabulary DeliverPlan's terminal-mapping switch matches against
// (docs/foundry/docs/architecture/state-model.md §2's registry).
const (
	ClassificationVerificationFailed Classification = "verification-failed"
	ClassificationPolicyViolation    Classification = "policy-violation"
	ClassificationRetryable          Classification = "retryable"
	ClassificationNoProgress         Classification = "no-progress"
	// ClassificationNoValidationDeclared is assigned when a task ran no
	// validation commands at all (docs/PLAN.md Task 104 / SKP-11R2). An empty
	// record set is NOT a pass: the honest-completion enforcement point must
	// never be bypassable by omission (Constitution C10). A task that
	// genuinely cannot be validated by command must declare the explicit
	// validation_optout in its plan and carry a human-recorded reason — it can
	// never auto-succeed through an absent record set.
	ClassificationNoValidationDeclared Classification = "no-validation-declared"
)

// Evaluate derives the sole, honest pass/fail verdict for a task from
// records — never from an executor's self-reported Summary (Constitution
// C10; docs/PLAN.md Task 13 Step 3: "kernel marks task result solely from
// Runner records; Summary stored but never trusted").
//
// attempt is the 1-indexed number of times this same validation has now
// run for the same task; it distinguishes a timeout's first occurrence
// (retryable) from a repeat (no-progress).
//
// Runner.Run stops at the first policy violation, timeout, or nonzero
// exit, so records is scanned in order and the first such record found is
// the one that determines the outcome.
func Evaluate(records []CommandRecord, attempt int) (ok bool, classification Classification) {
	// An empty record set means no validation command ran at all. That is not
	// a pass — it is the omission hole a lying executor would exploit, so it
	// fails closed with a distinct classification (docs/PLAN.md Task 104).
	if len(records) == 0 {
		return false, ClassificationNoValidationDeclared
	}
	for _, rec := range records {
		switch {
		case rec.PolicyViolation:
			return false, ClassificationPolicyViolation
		case rec.TimedOut:
			if attempt <= 1 {
				return false, ClassificationRetryable
			}
			return false, ClassificationNoProgress
		case rec.ExitCode != 0:
			return false, ClassificationVerificationFailed
		}
	}
	return true, ""
}
