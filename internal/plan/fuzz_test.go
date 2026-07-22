package plan

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse asserts ParseBytes never panics on arbitrary/malformed input —
// this package parses untrusted, user-authored Markdown, so a crash here
// would be a denial-of-service vector, not just a bug (docs/PLAN.md Task 6
// security scope: "fuzz-test robustness against malformed input").
func FuzzParse(f *testing.F) {
	seedDir := filepath.Join("..", "..", "examples", "plans")
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		f.Fatalf("read seed dir: %v", err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(seedDir, e.Name()))
		if err != nil {
			f.Fatalf("read seed %s: %v", e.Name(), err)
		}
		f.Add(raw)
	}

	f.Add([]byte(""))
	f.Add([]byte("---"))
	f.Add([]byte("---\n---\n"))
	f.Add([]byte("---\nid: [\n---\n"))
	f.Add([]byte("not front matter at all"))
	f.Add([]byte("---\nid: x\nversion: \"1\"\ntasks: {}\n---\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := ParseBytes(data)
		if err != nil {
			return
		}
		// A successful parse must satisfy its own invariants: Digest must
		// not panic, and every task id referenced by DependsOn must exist
		// (already enforced by validate, re-checked here as a fuzz
		// regression guard).
		_ = doc.DigestHex()

		ids := make(map[string]struct{}, len(doc.Tasks))
		for _, tsk := range doc.Tasks {
			ids[tsk.ID] = struct{}{}
		}
		for _, tsk := range doc.Tasks {
			for _, dep := range tsk.DependsOn {
				if _, ok := ids[dep]; !ok {
					t.Fatalf("accepted document with dangling depends_on %q", dep)
				}
			}
		}
	})
}
