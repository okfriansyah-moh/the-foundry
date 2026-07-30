package verify

import "testing"

func TestEvaluateAllPass(t *testing.T) {
	records := []CommandRecord{
		{Cmd: "go build ./...", ExitCode: 0},
		{Cmd: "go test ./...", ExitCode: 0},
	}
	ok, class := Evaluate(records, 1)
	if !ok || class != "" {
		t.Fatalf("Evaluate() = (%v, %q), want (true, \"\")", ok, class)
	}
}

func TestEvaluatePolicyViolationWins(t *testing.T) {
	records := []CommandRecord{
		{Cmd: "curl evil", PolicyViolation: true, ExitCode: -1},
	}
	ok, class := Evaluate(records, 1)
	if ok || class != ClassificationPolicyViolation {
		t.Fatalf("Evaluate() = (%v, %q), want (false, %q)", ok, class, ClassificationPolicyViolation)
	}
}

func TestEvaluateNonzeroExitIsVerificationFailed(t *testing.T) {
	records := []CommandRecord{
		{Cmd: "go test ./...", ExitCode: 1},
	}
	ok, class := Evaluate(records, 1)
	if ok || class != ClassificationVerificationFailed {
		t.Fatalf("Evaluate() = (%v, %q), want (false, %q)", ok, class, ClassificationVerificationFailed)
	}
}

func TestEvaluateTimeoutFirstAttemptIsRetryable(t *testing.T) {
	records := []CommandRecord{
		{Cmd: "go test ./...", TimedOut: true},
	}
	ok, class := Evaluate(records, 1)
	if ok || class != ClassificationRetryable {
		t.Fatalf("Evaluate() = (%v, %q), want (false, %q)", ok, class, ClassificationRetryable)
	}
}

func TestEvaluateTimeoutRepeatIsNoProgress(t *testing.T) {
	records := []CommandRecord{
		{Cmd: "go test ./...", TimedOut: true},
	}
	ok, class := Evaluate(records, 2)
	if ok || class != ClassificationNoProgress {
		t.Fatalf("Evaluate() = (%v, %q), want (false, %q)", ok, class, ClassificationNoProgress)
	}
}

// TestEvaluateIgnoresExecutorSummary proves the honest-completion contract
// (docs/PLAN.md Task 13 Step 3): even a "lying" executor Summary claiming
// success cannot change Evaluate's verdict, because Evaluate never looks
// at a Summary at all — it only ever sees CommandRecords, which is the
// entire point of the seam.
func TestEvaluateIgnoresExecutorSummary(t *testing.T) {
	lyingSummaryClaim := "all tests pass" // never passed to Evaluate — must not exist as a parameter it could read
	_ = lyingSummaryClaim

	records := []CommandRecord{
		{Cmd: "go test ./...", ExitCode: 1}, // the real, observed outcome
	}
	ok, class := Evaluate(records, 1)
	if ok {
		t.Fatal("Evaluate() reported ok=true for a nonzero exit code; a lying Summary must never be able to override real evidence")
	}
	if class != ClassificationVerificationFailed {
		t.Fatalf("Evaluate() classification = %q, want %q", class, ClassificationVerificationFailed)
	}
}

// TestEvaluateEmptyRecordsIsNoValidationDeclared proves the honest-completion
// closure (docs/PLAN.md Task 104): a task that ran no validation command must
// NOT pass — an empty record set fails closed with a distinct classification,
// so the enforcement point cannot be bypassed by omission (Constitution C10).
func TestEvaluateEmptyRecordsIsNoValidationDeclared(t *testing.T) {
	ok, class := Evaluate(nil, 1)
	if ok || class != ClassificationNoValidationDeclared {
		t.Fatalf("Evaluate(nil) = (%v, %q), want (false, %q)", ok, class, ClassificationNoValidationDeclared)
	}
	ok, class = Evaluate([]CommandRecord{}, 1)
	if ok || class != ClassificationNoValidationDeclared {
		t.Fatalf("Evaluate([]) = (%v, %q), want (false, %q)", ok, class, ClassificationNoValidationDeclared)
	}
}
