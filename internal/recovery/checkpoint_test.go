package recovery_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
)

func TestCheckpoint_ID_Deterministic(t *testing.T) {
	c := recovery.Checkpoint{LastCompletedTaskID: "t1", EvidenceIDs: []string{"bundle-a", "bundle-b"}}

	first := c.ID()
	second := recovery.Checkpoint{LastCompletedTaskID: "t1", EvidenceIDs: []string{"bundle-a", "bundle-b"}}.ID()

	if first != second {
		t.Fatalf("ID() not deterministic: %q != %q", first, second)
	}
	if first == "" {
		t.Fatal("ID() returned empty string")
	}
}

func TestCheckpoint_ID_OrderIndependent(t *testing.T) {
	a := recovery.Checkpoint{LastCompletedTaskID: "t2", EvidenceIDs: []string{"bundle-a", "bundle-b"}}.ID()
	b := recovery.Checkpoint{LastCompletedTaskID: "t2", EvidenceIDs: []string{"bundle-b", "bundle-a"}}.ID()

	if a != b {
		t.Fatalf("ID() depends on EvidenceIDs order: %q != %q", a, b)
	}
}

func TestCheckpoint_ID_DiffersOnTaskOrEvidence(t *testing.T) {
	base := recovery.Checkpoint{LastCompletedTaskID: "t1", EvidenceIDs: []string{"bundle-a"}}.ID()

	diffTask := recovery.Checkpoint{LastCompletedTaskID: "t2", EvidenceIDs: []string{"bundle-a"}}.ID()
	if diffTask == base {
		t.Fatal("ID() did not change when LastCompletedTaskID changed")
	}

	diffEvidence := recovery.Checkpoint{LastCompletedTaskID: "t1", EvidenceIDs: []string{"bundle-a", "bundle-b"}}.ID()
	if diffEvidence == base {
		t.Fatal("ID() did not change when EvidenceIDs changed")
	}
}

func TestCheckpoint_ID_EmptyCheckpointIsStable(t *testing.T) {
	first := recovery.Checkpoint{}.ID()
	second := recovery.Checkpoint{}.ID()
	if first != second || first == "" {
		t.Fatalf("zero-value Checkpoint.ID() = %q, %q, want equal non-empty values", first, second)
	}
}
