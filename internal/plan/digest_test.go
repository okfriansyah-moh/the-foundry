package plan

import (
	"strings"
	"testing"
)

func TestDigestStableAcrossLineEndingsAndTrailingSpace(t *testing.T) {
	base := readExample(t, "hello-world.md")

	crlf := strings.ReplaceAll(string(base), "\n", "\r\n")
	withTrailingSpace := insertTrailingSpaces(string(base))

	docBase, err := ParseBytes(base)
	if err != nil {
		t.Fatalf("ParseBytes(base): %v", err)
	}
	docCRLF, err := ParseBytes([]byte(crlf))
	if err != nil {
		t.Fatalf("ParseBytes(crlf): %v", err)
	}
	docTrailing, err := ParseBytes([]byte(withTrailingSpace))
	if err != nil {
		t.Fatalf("ParseBytes(trailingSpace): %v", err)
	}

	want := docBase.DigestHex()
	if got := docCRLF.DigestHex(); got != want {
		t.Errorf("CRLF digest = %s, want %s (line-ending permutation must not change digest)", got, want)
	}
	if got := docTrailing.DigestHex(); got != want {
		t.Errorf("trailing-space digest = %s, want %s (trailing whitespace must not change digest)", got, want)
	}
}

// insertTrailingSpaces appends trailing spaces/tabs to every non-empty line,
// simulating an editor artifact that the canonical digest must ignore.
func insertTrailingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = line + "   \t"
		}
	}
	return strings.Join(lines, "\n")
}

func TestDigestChangesOnContentChange(t *testing.T) {
	docA, err := ParseBytes(readExample(t, "hello-world.md"))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	docB, err := ParseBytes(readExample(t, "two-task.md"))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if docA.DigestHex() == docB.DigestHex() {
		t.Error("digests of different plans must not collide")
	}
}

func TestDigestSerializeReparseStable(t *testing.T) {
	for _, name := range []string{"hello-world.md", "two-task.md", "failing-task.md"} {
		t.Run(name, func(t *testing.T) {
			doc1, err := ParseBytes(readExample(t, name))
			if err != nil {
				t.Fatalf("ParseBytes: %v", err)
			}

			serialized, err := doc1.Serialize()
			if err != nil {
				t.Fatalf("Serialize: %v", err)
			}

			doc2, err := ParseBytes(serialized)
			if err != nil {
				t.Fatalf("ParseBytes(serialized): %v\n--- serialized ---\n%s", err, serialized)
			}

			// Re-serializing the reparsed document must be a fixed point:
			// Serialize is deterministic and canonical, so a second round
			// trip produces byte-identical output and therefore an
			// identical digest.
			reserialized, err := doc2.Serialize()
			if err != nil {
				t.Fatalf("Serialize (round 2): %v", err)
			}
			if string(reserialized) != string(serialized) {
				t.Fatalf("Serialize is not idempotent:\n--- round 1 ---\n%s\n--- round 2 ---\n%s", serialized, reserialized)
			}

			doc3, err := ParseBytes(reserialized)
			if err != nil {
				t.Fatalf("ParseBytes(reserialized): %v", err)
			}
			if doc2.DigestHex() != doc3.DigestHex() {
				t.Errorf("digest not stable across reserialize/reparse: %s != %s", doc2.DigestHex(), doc3.DigestHex())
			}
		})
	}
}
