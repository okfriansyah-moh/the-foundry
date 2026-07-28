package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// Memory is one curated, provenance-stamped piece of knowledge.
type Memory struct {
	// ID is a stable content-addressed identifier (see MemoryID).
	ID string
	// Content is the curated knowledge text.
	Content string
	// Kind categorizes the memory (e.g. "convention", "fact", "preference").
	Kind string
	// ProfileScope is the single profile that owns this memory. It is never
	// readable from another profile.
	ProfileScope string
	// Confidence is a 0..1 score of how strongly the evidence supports it.
	Confidence float64
	// EvidenceRefs are the source evidence identifiers this memory derives
	// from — its provenance. Deleting any of them cascades to this memory.
	EvidenceRefs []string
	// TTL, when > 0, bounds the memory's lifetime; ExpiresAt is derived from
	// CreatedAt + TTL. Zero TTL means no expiry.
	TTL time.Duration
	// CreatedAt is when the memory was first stored (UTC).
	CreatedAt time.Time
	// ExpiresAt is CreatedAt+TTL for a positive TTL, else the zero time.
	ExpiresAt time.Time
}

// Candidate is a proposed memory emitted by a Proposer, before dedupe/merge
// and storage.
type Candidate struct {
	Content      string
	Kind         string
	ProfileScope string
	Confidence   float64
	TTL          time.Duration
	EvidenceRefs []string
}

// EvidenceInput is one source-evidence item fed to the curator.
type EvidenceInput struct {
	// Ref is the source evidence identifier (e.g. an evidence bundle ID).
	Ref string
	// Text is the evidence content the Proposer reasons over.
	Text string
}

// normalizeContent produces the dedupe key for a memory/candidate: trimmed,
// lowercased, whitespace-collapsed content. Two candidates that normalize to
// the same key are merged rather than stored twice.
func normalizeContent(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// MemoryID is the stable, content-addressed ID for a memory in a profile.
// It is derived from the owning profile plus normalized content, so the same
// knowledge in the same profile always maps to the same ID (enabling
// idempotent dedupe) while identical text in two profiles stays distinct.
func MemoryID(profileScope, content string) string {
	sum := sha256.Sum256([]byte(profileScope + "\x00" + normalizeContent(content)))
	return hex.EncodeToString(sum[:])
}

// mergeRefs returns the sorted, de-duplicated union of two evidence-ref sets.
func mergeRefs(a, b []string) []string {
	set := map[string]struct{}{}
	for _, r := range a {
		set[r] = struct{}{}
	}
	for _, r := range b {
		set[r] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
