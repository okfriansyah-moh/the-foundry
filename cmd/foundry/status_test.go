package main

import (
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func TestParseStatusArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantWF    string
		wantFresh bool
		wantErr   bool
	}{
		{name: "workflow only", args: []string{"wf-1"}, wantWF: "wf-1", wantFresh: false},
		{name: "flag after workflow", args: []string{"wf-1", "--fresh"}, wantWF: "wf-1", wantFresh: true},
		{name: "flag before workflow", args: []string{"--fresh", "wf-1"}, wantWF: "wf-1", wantFresh: true},
		{name: "dsn flags either side", args: []string{"--pg-dsn=postgres://x", "wf-1", "--fresh"}, wantWF: "wf-1", wantFresh: true},
		{name: "missing workflow id", args: []string{"--fresh"}, wantErr: true},
		{name: "empty args", args: []string{}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStatusArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseStatusArgs(%v) = nil error, want error", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStatusArgs(%v) unexpected error: %v", tc.args, err)
			}
			if got.workflowID != tc.wantWF {
				t.Errorf("workflowID = %q, want %q", got.workflowID, tc.wantWF)
			}
			if got.fresh != tc.wantFresh {
				t.Errorf("fresh = %v, want %v", got.fresh, tc.wantFresh)
			}
		})
	}
}

func TestParseStatusArgsDefaults(t *testing.T) {
	got, err := parseStatusArgs([]string{"wf-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.pgDSN == "" {
		t.Error("pgDSN default should not be empty")
	}
	if got.temporalHostPort == "" {
		t.Error("temporalHostPort default should not be empty")
	}
	if got.temporalNS != "default" {
		t.Errorf("temporalNS default = %q, want \"default\"", got.temporalNS)
	}
}

func TestProjectionLag(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 30, 0, time.UTC)

	cases := []struct {
		name      string
		updatedAt time.Time
		want      time.Duration
	}{
		{name: "30s stale", updatedAt: now.Add(-30 * time.Second), want: 30 * time.Second},
		{name: "fresh (zero lag)", updatedAt: now, want: 0},
		{name: "future timestamp clamps to zero", updatedAt: now.Add(5 * time.Second), want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectionLag(tc.updatedAt, now)
			if got != tc.want {
				t.Errorf("projectionLag(%v, %v) = %v, want %v", tc.updatedAt, now, got, tc.want)
			}
		})
	}
}

func TestFormatProjected(t *testing.T) {
	row := projectionRow{
		WorkflowID: "wf-1",
		Status:     "RUNNING",
		Phase:      "executing",
		LastSeq:    42,
	}
	out := formatProjected(row, 17*time.Second)

	for _, want := range []string{
		"workflow_id: wf-1",
		"status: RUNNING",
		"phase: executing",
		"last_seq: 42",
		"consistency: projected (lag: 17s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatProjected output missing %q; got:\n%s", want, out)
		}
	}
}

func TestFormatFresh(t *testing.T) {
	last := state.Transition{
		Status:  state.StatusRunning,
		PhaseTo: "executing",
	}
	out := formatFresh("wf-1", "WORKFLOW_EXECUTION_STATUS_RUNNING", last)

	for _, want := range []string{
		"workflow_id: wf-1",
		"status: RUNNING",
		"phase: executing",
		"temporal_status: WORKFLOW_EXECUTION_STATUS_RUNNING",
		"consistency: fresh",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFresh output missing %q; got:\n%s", want, out)
		}
	}

	// The whole point of --fresh is that it never claims "projected" in its
	// output label (docs/foundry/docs/architecture/data-consistency.md §2).
	if strings.Contains(out, "projected") {
		t.Errorf("formatFresh output must not mention 'projected'; got:\n%s", out)
	}
}
