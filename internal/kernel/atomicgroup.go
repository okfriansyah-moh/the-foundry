package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// FileAction describes what happened to one file in a change set.
type FileAction string

const (
	FileActionAdded    FileAction = "added"
	FileActionModified FileAction = "modified"
	FileActionDeleted  FileAction = "deleted"
)

// FileChange is one file entry in a ChangeSet manifest.
type FileChange struct {
	Path    string     `json:"path"`
	Action  FileAction `json:"action"`
	BlobSHA string     `json:"blob_sha,omitempty"` // empty for deleted
}

// ChangeSet is the machine-verifiable manifest of one AtomicGroup's changes.
// The digest of a ChangeSet is embedded in the group's tip commit trailer
// ("Foundry-Changeset: <digest>") for downstream traceability.
type ChangeSet struct {
	Files             []FileChange `json:"files"`
	Tests             []string     `json:"tests"`              // test commands that were run
	ValidationRecords []string     `json:"validation_records"` // ref to verify.CommandRecord IDs
}

// Digest returns the deterministic SHA-256 hex digest of the ChangeSet.
// The digest is stable: all slice fields are sorted before hashing so
// insertion order does not affect the result (Constitution C10: determinism).
func (cs ChangeSet) Digest() string {
	// Normalize: sort files by path, tests and records alphabetically.
	files := make([]FileChange, len(cs.Files))
	copy(files, cs.Files)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	tests := append([]string{}, cs.Tests...)
	sort.Strings(tests)

	valRecs := append([]string{}, cs.ValidationRecords...)
	sort.Strings(valRecs)

	normalized := struct {
		Files             []FileChange `json:"files"`
		Tests             []string     `json:"tests"`
		ValidationRecords []string     `json:"validation_records"`
	}{files, tests, valRecs}

	raw, err := json.Marshal(normalized)
	if err != nil {
		// json.Marshal should never fail on this well-typed struct;
		// panic loudly rather than silently hash nil (would produce a
		// misleading but deterministic digest — Constitution C10).
		panic(fmt.Sprintf("atomicgroup: ChangeSet.Digest marshal failed: %v", err))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// AtomicGroup is one coherent, reviewable, deterministically-checked group
// of commits that the Branch Integrator pushes as a unit (Task 58 / TX-05).
//
// The manifest is embedded in a trailer on the group's tip commit:
//
//	Foundry-Changeset: <ChangeSet.Digest()>
//
// One commit per plan task (default per Task 57 Steps); the group boundary
// is recorded in the manifest only, not as an extra marker commit.
type AtomicGroup struct {
	// ID uniquely identifies this group.
	ID string `json:"id"`
	// PlanTaskIDs are the plan task IDs covered by this group.
	PlanTaskIDs []string `json:"plan_task_ids"`
	// Commits is the list of git SHAs belonging to this group, in order.
	Commits []string `json:"commits"`
	// Manifest is the computed change-set manifest.
	Manifest ChangeSet `json:"manifest"`
}

// ManifestDigest returns the embedded trailer value for the tip commit.
func (g AtomicGroup) ManifestDigest() string {
	return g.Manifest.Digest()
}

// TipCommitTrailer returns the "Foundry-Changeset: <digest>" trailer line
// that must be appended to the tip commit message.
func (g AtomicGroup) TipCommitTrailer() string {
	return "Foundry-Changeset: " + g.ManifestDigest()
}

// DeclaredScope returns the set of file path prefixes the plan tasks declare.
// The scope guard uses this to verify no file in the ChangeSet is outside scope.
type DeclaredScope struct {
	// Prefixes is the union of all plan.Task.Files patterns for this group.
	Prefixes []string
}

// Contains reports whether path is within declared scope. A path is in scope
// if it matches any prefix (simple prefix match; "**" stripped for matching).
func (s DeclaredScope) Contains(path string) bool {
	for _, prefix := range s.Prefixes {
		clean := strings.TrimSuffix(prefix, "/**")
		clean = strings.TrimSuffix(clean, "/*")
		if strings.HasPrefix(path, clean) {
			return true
		}
	}
	return false
}

// ValidateScope checks that every file in g.Manifest is within the declared
// scope. Returns an error naming the first out-of-scope file.
// An out-of-scope change results in FAILED/policy-violation (Constitution C4
// and Task 57 scope-guard requirement).
func (g AtomicGroup) ValidateScope(scope DeclaredScope) error {
	for _, f := range g.Manifest.Files {
		if !scope.Contains(f.Path) {
			return &ScopeViolationError{Path: f.Path, GroupID: g.ID}
		}
	}
	return nil
}

// ScopeViolationError is returned when an AtomicGroup's manifest contains
// a file outside the plan-declared scope.
type ScopeViolationError struct {
	Path    string
	GroupID string
}

func (e *ScopeViolationError) Error() string {
	return fmt.Sprintf("kernel: atomic group %q: file %q is outside plan-declared scope (policy-violation)", e.GroupID, e.Path)
}
