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
