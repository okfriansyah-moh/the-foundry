package state

import (
	"errors"
	"testing"
)

func TestTransitionValidate_LegalGraph(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		t       Transition
		wantErr bool
	}{
		{"pending to running", StatusPending, StatusRunning, Transition{}, false},
		{"running to waiting with reason", StatusRunning, StatusWaiting, Transition{Reason: ReasonBudget}, false},
		{"waiting to running", StatusWaiting, StatusRunning, Transition{}, false},
		{"running to succeeded", StatusRunning, StatusSucceeded, Transition{}, false},
		{"running to succeeded with result", StatusRunning, StatusSucceeded, Transition{ResultCode: ResultMissionTargetReached}, false},
		{"running to failed", StatusRunning, StatusFailed, Transition{}, false},
		{"running to failed with known result", StatusRunning, StatusFailed, Transition{ResultCode: ResultProvenBlocked}, false},
		{"running to cancelled", StatusRunning, StatusCancelled, Transition{}, false},
		{"waiting to cancelled", StatusWaiting, StatusCancelled, Transition{}, false},
		{"waiting to failed", StatusWaiting, StatusFailed, Transition{}, false},

		{"pending to waiting illegal", StatusPending, StatusWaiting, Transition{Reason: ReasonBudget}, true},
		{"pending to succeeded illegal", StatusPending, StatusSucceeded, Transition{}, true},
		{"pending to failed illegal", StatusPending, StatusFailed, Transition{}, true},
		{"pending to cancelled illegal", StatusPending, StatusCancelled, Transition{}, true},
		{"running to pending illegal", StatusRunning, StatusPending, Transition{}, true},
		{"waiting to pending illegal", StatusWaiting, StatusPending, Transition{}, true},

		{"succeeded absorbs to running", StatusSucceeded, StatusRunning, Transition{}, true},
		{"succeeded absorbs to waiting", StatusSucceeded, StatusWaiting, Transition{Reason: ReasonBudget}, true},
		{"succeeded absorbs to failed", StatusSucceeded, StatusFailed, Transition{}, true},
		{"failed absorbs to running", StatusFailed, StatusRunning, Transition{}, true},
		{"failed absorbs to cancelled", StatusFailed, StatusCancelled, Transition{}, true},
		{"cancelled absorbs to running", StatusCancelled, StatusRunning, Transition{}, true},
		{"cancelled absorbs to waiting", StatusCancelled, StatusWaiting, Transition{Reason: ReasonBudget}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.t.Validate(tc.from, tc.to)
			if tc.wantErr && err == nil {
				t.Fatalf("Validate(%s, %s) = nil, want error", tc.from, tc.to)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate(%s, %s) = %v, want nil", tc.from, tc.to, err)
			}
		})
	}
}

func TestTransitionValidate_IllegalTransitionSentinel(t *testing.T) {
	err := (Transition{}).Validate(StatusPending, StatusFailed)
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestTransitionValidate_Invariants(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		t       Transition
		wantErr bool
	}{
		{"waiting without reason is illegal", StatusRunning, StatusWaiting, Transition{}, true},
		{"waiting with reason is legal", StatusRunning, StatusWaiting, Transition{Reason: ReasonHumanApproval}, false},
		{"succeeded with reason is illegal", StatusRunning, StatusSucceeded, Transition{Reason: ReasonBudget}, true},
		{"succeeded without reason is legal", StatusRunning, StatusSucceeded, Transition{}, false},
		{"failed with unknown result code is illegal", StatusRunning, StatusFailed, Transition{ResultCode: ResultCode("NOT_REGISTERED")}, true},
		{"failed with mismatched status result code is illegal", StatusRunning, StatusFailed, Transition{ResultCode: ResultMissionTargetReached}, true},
		{"failed with matching result code is legal", StatusRunning, StatusFailed, Transition{ResultCode: ResultAdmissionRejected}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.t.Validate(tc.from, tc.to)
			if tc.wantErr {
				if !errors.Is(err, ErrInvariantViolation) {
					t.Fatalf("expected ErrInvariantViolation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStatusIsTerminal(t *testing.T) {
	terminal := []Status{StatusSucceeded, StatusFailed, StatusCancelled}
	nonTerminal := []Status{StatusPending, StatusRunning, StatusWaiting}

	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s: want terminal, got non-terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s: want non-terminal, got terminal", s)
		}
	}
}
