package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// CommandRecord is one command that ran as part of proving a task, and its
// observed outcome. StdoutDigest is a sha256 hex digest of the command's
// captured stdout, not the raw output, so bundles stay small and never leak
// full command output into evidence storage.
type CommandRecord struct {
	Cmd          string
	ExitCode     int
	StdoutDigest string
	DurationMS   int64
}

// ArtifactRef points at one artifact file within a Bundle's Dir and records
// its content hash and size at the time the bundle was built. Path is
// relative to the bundle's artifacts/ directory.
type ArtifactRef struct {
	Path   string
	SHA256 string
	Bytes  int64
}

// Manifest is the canonical, hashable description of a Bundle's contents.
// Its digest (ManifestDigest) is computed over the canonical JSON encoding
// of this struct and is the value Store.Put uses as the bundle ID.
type Manifest struct {
	WorkflowID string
	TaskID     string
	// ExecutorUsed names the executor adapter the kernel selected to run
	// this task (docs/PLAN.md Task 85 / PRV-02, Step 3). omitempty keeps
	// the digest of every pre-Task-85 bundle (which never set it)
	// byte-identical — the field is additive and non-breaking.
	ExecutorUsed string `json:",omitempty"`
	// Profile is the profile namespace this bundle was produced under
	// (docs/PLAN.md Task 118 / SEC-04). omitempty keeps every pre-Task-118
	// bundle's digest byte-identical — the field is additive and non-breaking.
	// Migration note: existing bundles carry no profile and are treated as
	// personal-profile by definition (recorded decision, not a rewrite).
	Profile string `json:",omitempty"`
	// EnvelopeDigest is the Task 141 execution-envelope digest that authorized
	// this evidence bundle. omitempty keeps pre-Task-141 digests stable.
	EnvelopeDigest string `json:",omitempty"`
	Commands       []CommandRecord
	Artifacts      []ArtifactRef
	Transitions    []state.Transition
	CreatedAt      time.Time
}

// canonicalJSON renders m deterministically: json.Marshal already emits
// compact output (no indentation) and preserves declared struct-field
// order (Manifest contains no maps), so it is stable across processes and
// platforms without further normalization.
func (m Manifest) canonicalJSON() ([]byte, error) {
	buf, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("evidence: marshal manifest: %w", err)
	}
	return buf, nil
}

// Digest returns the sha256 digest of m's canonical JSON encoding.
func (m Manifest) Digest() ([32]byte, error) {
	canon, err := m.canonicalJSON()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(canon), nil
}

// DigestHex returns Digest as a lowercase hex string.
func (m Manifest) DigestHex() (string, error) {
	sum, err := m.Digest()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

// Bundle pairs a Manifest with the on-disk directory containing its
// artifacts, as produced or loaded by a Store.
type Bundle struct {
	Manifest Manifest
	Dir      string
}
