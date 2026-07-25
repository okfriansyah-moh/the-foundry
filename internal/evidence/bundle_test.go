package evidence

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func testManifest() Manifest {
	return Manifest{
		WorkflowID: "wf-1",
		TaskID:     "task-11",
		Commands: []CommandRecord{
			{Cmd: "go test ./...", ExitCode: 0, StdoutDigest: "abc123", DurationMS: 42},
		},
		Artifacts: []ArtifactRef{
			{Path: "output.txt", SHA256: "deadbeef", Bytes: 4},
		},
		Transitions: []state.Transition{
			{WorkflowID: "wf-1", Status: state.StatusSucceeded, OccurredAt: time.Unix(0, 0).UTC()},
		},
		CreatedAt: time.Unix(0, 0).UTC(),
	}
}

func TestManifestDigestDeterministic(t *testing.T) {
	m1 := testManifest()
	m2 := testManifest()

	d1, err := m1.DigestHex()
	if err != nil {
		t.Fatalf("digest 1: %v", err)
	}
	d2, err := m2.DigestHex()
	if err != nil {
		t.Fatalf("digest 2: %v", err)
	}
	if d1 != d2 {
		t.Errorf("identical manifests produced different digests: %s != %s", d1, d2)
	}
}

func TestManifestDigestChangesOnContentChange(t *testing.T) {
	m1 := testManifest()
	m2 := testManifest()
	m2.TaskID = "task-12"

	d1, _ := m1.DigestHex()
	d2, _ := m2.DigestHex()
	if d1 == d2 {
		t.Errorf("different manifests produced the same digest: %s", d1)
	}
}
