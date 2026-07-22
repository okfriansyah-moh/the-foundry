package state

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// governingDocPath resolves docs/foundry/docs/architecture/state-model.md
// relative to this test file, independent of the test runner's working
// directory.
func governingDocPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "docs", "foundry", "docs", "architecture", "state-model.md")
}

// TestRegistriesMatchGoverningDoc is the golden doc-diff test required by
// Task 5's Acceptance: it reads state-model.md §2 directly and diffs the
// phase, wait-reason, and terminal result-code registries against this
// package's code, so registry drift fails the test rather than silently
// diverging from the governing doc.
func TestRegistriesMatchGoverningDoc(t *testing.T) {
	raw, err := os.ReadFile(governingDocPath(t))
	if err != nil {
		t.Fatalf("read governing doc: %v", err)
	}
	doc := string(raw)

	t.Run("phase registry", func(t *testing.T) {
		re := regexp.MustCompile(`\*\*Phase registry \(extensible via governance, not ad hoc\):\*\*\s*([^\n]+)\.`)
		m := re.FindStringSubmatch(doc)
		if m == nil {
			t.Fatal("phase registry line not found in governing doc")
		}
		want := splitCSV(m[1])
		got := make([]string, len(phaseRegistry))
		for i, p := range phaseRegistry {
			got[i] = string(p)
		}
		assertEqualSlices(t, want, got)
	})

	t.Run("wait-reason registry", func(t *testing.T) {
		re := regexp.MustCompile(`\*\*Wait-reason registry:\*\*\s*([^\n]+)\.`)
		m := re.FindStringSubmatch(doc)
		if m == nil {
			t.Fatal("wait-reason registry line not found in governing doc")
		}
		want := splitCSV(m[1])
		got := make([]string, len(reasonRegistry))
		for i, r := range reasonRegistry {
			got[i] = string(r)
		}
		assertEqualSlices(t, want, got)
	})

	t.Run("terminal result-code registry", func(t *testing.T) {
		re := regexp.MustCompile("(?s)Terminal result-code registry \\(initial\\):\\*\\*\\s*```text\\s*(.*?)```")
		m := re.FindStringSubmatch(doc)
		if m == nil {
			t.Fatal("terminal result-code registry code block not found in governing doc")
		}
		lineRe := regexp.MustCompile(`^(\S+)\s+on\s+(FAILED|SUCCEEDED|CANCELLED)\b`)
		var wantCodes []string
		wantStatus := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(m[1]), "\n") {
			lm := lineRe.FindStringSubmatch(strings.TrimSpace(line))
			if lm == nil {
				continue
			}
			wantCodes = append(wantCodes, lm[1])
			wantStatus[lm[1]] = lm[2]
		}
		if len(wantCodes) == 0 {
			t.Fatal("no result-code lines parsed from governing doc")
		}

		gotCodes := make([]string, len(resultCodeRegistry))
		for i, e := range resultCodeRegistry {
			gotCodes[i] = string(e.Code)
			if want, ok := wantStatus[string(e.Code)]; ok && want != string(e.Status) {
				t.Errorf("result code %s: doc says on %s, code registers on %s", e.Code, want, e.Status)
			}
		}
		assertEqualSlices(t, wantCodes, gotCodes)
	})
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func assertEqualSlices(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("length mismatch: doc has %d, code has %d\ndoc:  %v\ncode: %v", len(want), len(got), want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("mismatch at index %d: doc=%q code=%q\ndoc:  %v\ncode: %v", i, want[i], got[i], want, got)
		}
	}
}
