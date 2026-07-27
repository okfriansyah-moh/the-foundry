package recovery_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

func TestEvaluate_MissingDependencyProducesProvenBlocked(t *testing.T) {
	history := []recovery.FailureSignature{
		{Classification: verify.ClassificationRetryable, Detail: "connect: timeout"},
		{
			Classification:    verify.ClassificationVerificationFailed,
			Detail:            "task-9 never produced output.json",
			MissingDependency: true,
			EvidenceRefs:      []string{"evidence-bundle-1"},
		},
	}

	blocked, ok := recovery.Evaluate(history)
	if !ok {
		t.Fatal("Evaluate() ok = false, want true")
	}
	if blocked.ResultCode != state.ResultProvenBlocked {
		t.Fatalf("ResultCode = %q, want %q", blocked.ResultCode, state.ResultProvenBlocked)
	}
	if blocked.Reason != "missing-dependency" {
		t.Fatalf("Reason = %q, want missing-dependency", blocked.Reason)
	}
	if blocked.NextAction == "" {
		t.Fatal("NextAction is empty, want operator guidance")
	}
	if len(blocked.EvidenceRefs) == 0 {
		t.Fatal("EvidenceRefs is empty, want the attached refs")
	}
	if err := blocked.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestEvaluate_ContradictorySpecProducesProvenBlocked(t *testing.T) {
	history := []recovery.FailureSignature{
		{
			Classification:    verify.ClassificationVerificationFailed,
			ContradictorySpec: true,
			EvidenceRefs:      []string{"evidence-bundle-2"},
		},
	}

	blocked, ok := recovery.Evaluate(history)
	if !ok {
		t.Fatal("Evaluate() ok = false, want true")
	}
	if blocked.Reason != "contradictory-spec" {
		t.Fatalf("Reason = %q, want contradictory-spec", blocked.Reason)
	}
}

func TestEvaluate_NoImpossibilityFlagIsNotProvenBlocked(t *testing.T) {
	history := []recovery.FailureSignature{
		{Classification: verify.ClassificationRetryable, Detail: "a"},
		{Classification: verify.ClassificationRetryable, Detail: "b"},
		{Classification: verify.ClassificationRetryable, Detail: "c"},
	}

	_, ok := recovery.Evaluate(history)
	if ok {
		t.Fatal("Evaluate() ok = true, want false: budget exhaustion alone is not proof of impossibility")
	}
}

func TestEvaluate_ImpossibilityFlagWithNoEvidenceIsNotProvenBlocked(t *testing.T) {
	history := []recovery.FailureSignature{
		{
			Classification:    verify.ClassificationVerificationFailed,
			MissingDependency: true,
			// EvidenceRefs deliberately empty: Evaluate must not hand back a
			// hollow PROVEN_BLOCKED just because the impossibility flag is
			// set — Validate()'s evidence requirement is enforced inside
			// Evaluate itself, not left to every future caller.
		},
	}

	blocked, ok := recovery.Evaluate(history)
	if ok {
		t.Fatalf("Evaluate() ok = true, want false: no EvidenceRefs means Validate() must reject it internally; got %+v", blocked)
	}
}

func TestEvaluate_SkipsUnvalidatableSignatureAndUsesLaterValidOne(t *testing.T) {
	history := []recovery.FailureSignature{
		{
			Classification:    verify.ClassificationVerificationFailed,
			MissingDependency: true,
			// no EvidenceRefs: must be skipped internally by Evaluate, not
			// returned and not treated as a terminal "no impossibility found".
		},
		{
			Classification:    verify.ClassificationVerificationFailed,
			ContradictorySpec: true,
			EvidenceRefs:      []string{"evidence-bundle-3"},
		},
	}

	blocked, ok := recovery.Evaluate(history)
	if !ok {
		t.Fatal("Evaluate() ok = false, want true: the second signature has valid evidence")
	}
	if blocked.Reason != "contradictory-spec" {
		t.Fatalf("Reason = %q, want contradictory-spec (from the second, evidence-bearing signature)", blocked.Reason)
	}
	if err := blocked.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestProvenBlocked_ValidateRejectsMissingEvidence(t *testing.T) {
	blocked := recovery.ProvenBlocked{
		ResultCode: state.ResultProvenBlocked,
		NextAction: "operator: investigate",
	}
	if err := blocked.Validate(); err == nil {
		t.Fatal("Validate() = nil, want error: no EvidenceRefs")
	}
}

func TestProvenBlocked_ValidateRejectsWrongResultCode(t *testing.T) {
	blocked := recovery.ProvenBlocked{
		ResultCode:   state.ResultAdmissionRejected,
		NextAction:   "operator: investigate",
		EvidenceRefs: []string{"e1"},
	}
	if err := blocked.Validate(); err == nil {
		t.Fatal("Validate() = nil, want error: wrong ResultCode")
	}
}

func TestProvenBlocked_ValidateRejectsMissingNextAction(t *testing.T) {
	blocked := recovery.ProvenBlocked{
		ResultCode:   state.ResultProvenBlocked,
		EvidenceRefs: []string{"e1"},
	}
	if err := blocked.Validate(); err == nil {
		t.Fatal("Validate() = nil, want error: no NextAction")
	}
}
