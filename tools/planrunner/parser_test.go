package main

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T) *Plan {
	t.Helper()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "fixture_plan.md")
	src, err := os.ReadFile("testdata/fixture_plan.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("write scratch fixture: %v", err)
	}
	plan, err := ParsePlan(dst)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	return plan
}

func TestParsePlanIndex(t *testing.T) {
	plan := loadFixture(t)
	if len(plan.Index) != 5 {
		t.Fatalf("want 5 index rows, got %d", len(plan.Index))
	}

	byTask := map[int]IndexRow{}
	for _, row := range plan.Index {
		byTask[row.Task] = row
	}

	if !byTask[1].Done {
		t.Errorf("task 1 should be Done")
	}
	if byTask[2].Done {
		t.Errorf("task 2 should not be Done")
	}
	if got := byTask[2].Depends; len(got) != 1 || got[0] != 1 {
		t.Errorf("task 2 Depends = %v, want [1]", got)
	}
	if got := byTask[5].Depends; len(got) != 1 || got[0] != 99 {
		t.Errorf("task 5 Depends = %v, want [99]", got)
	}
}

func TestParsePlanCards(t *testing.T) {
	plan := loadFixture(t)

	card2 := plan.Cards[2]
	if card2 == nil {
		t.Fatalf("card 2 not parsed")
	}
	if card2.Risk != "Low" {
		t.Errorf("card 2 Risk = %q, want Low", card2.Risk)
	}
	if card2.Rev != "R1" {
		t.Errorf("card 2 Rev = %q, want R1", card2.Rev)
	}
	if card2.Outputs != "`fixture/low.txt`" {
		t.Errorf("card 2 Outputs = %q, want `fixture/low.txt`", card2.Outputs)
	}
	if len(card2.Validation) != 1 || card2.Validation[0] != "true" {
		t.Errorf("card 2 Validation = %v, want [true]", card2.Validation)
	}

	card3 := plan.Cards[3]
	if card3.Risk != "High" || card3.Rev != "R3" {
		t.Errorf("card 3 Risk/Rev = %q/%q, want High/R3", card3.Risk, card3.Rev)
	}
}

func TestPlanEligible(t *testing.T) {
	plan := loadFixture(t)
	eligible := plan.Eligible()

	got := map[int]bool{}
	for _, row := range eligible {
		got[row.Task] = true
	}
	for _, want := range []int{2, 3, 4} {
		if !got[want] {
			t.Errorf("expected task %d to be eligible, eligible set = %v", want, got)
		}
	}
	if got[5] {
		t.Errorf("task 5 depends on 99 (never done) and must not be eligible")
	}
	if got[1] {
		t.Errorf("task 1 is already Done and must not be eligible")
	}
}

func TestPlanMarkDone(t *testing.T) {
	plan := loadFixture(t)

	if err := plan.MarkDone(2, "2026-07-21"); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	// Re-parse from disk to prove the write actually landed, not just the in-memory copy.
	reloaded, err := ParsePlan(plan.Path)
	if err != nil {
		t.Fatalf("re-parse after MarkDone: %v", err)
	}

	for _, row := range reloaded.Index {
		if row.Task == 2 && !row.Done {
			t.Errorf("task 2 Index row should be Done after MarkDone")
		}
	}

	eligible := map[int]bool{}
	for _, row := range reloaded.Eligible() {
		eligible[row.Task] = true
	}
	if eligible[2] {
		t.Errorf("task 2 should no longer be eligible after MarkDone")
	}

	// Task 3's status line must be untouched.
	if reloaded.Cards[3] == nil || reloaded.Cards[3].Risk != "High" {
		t.Fatalf("MarkDone(2) must not disturb task 3's card")
	}
}

func TestPlanMarkDoneUnknownTask(t *testing.T) {
	plan := loadFixture(t)
	if err := plan.MarkDone(999, "2026-07-21"); err == nil {
		t.Fatalf("MarkDone(999) should error for a task that does not exist")
	}
}
