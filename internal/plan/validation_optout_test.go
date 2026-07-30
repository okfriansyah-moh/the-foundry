package plan

import "testing"

// TestValidationRequiredOrOptOut proves docs/PLAN.md Task 104's honest-
// completion tightening at the plan-schema level: a task must declare at least
// one validation command, or take the explicit, reasoned opt-out.
func TestValidationRequiredOrOptOut(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{
			name: "with validation commands",
			src:  "---\nid: p\nversion: \"1.0\"\ntasks:\n  - id: t1\n    goal: g\n    validation_commands:\n      - echo ok\n---\n",
		},
		{
			name: "opt-out with reason",
			src:  "---\nid: p\nversion: \"1.0\"\ntasks:\n  - id: t1\n    goal: g\n    validation_optout: true\n    validation_optout_reason: cannot be validated by command\n---\n",
		},
		{
			name:    "no commands and no opt-out is rejected",
			src:     "---\nid: p\nversion: \"1.0\"\ntasks:\n  - id: t1\n    goal: g\n---\n",
			wantErr: true,
		},
		{
			name:    "opt-out without a reason is rejected",
			src:     "---\nid: p\nversion: \"1.0\"\ntasks:\n  - id: t1\n    goal: g\n    validation_optout: true\n---\n",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBytes([]byte(tc.src))
			if tc.wantErr && err == nil {
				t.Fatalf("expected a validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
