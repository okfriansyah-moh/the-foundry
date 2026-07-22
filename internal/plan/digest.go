package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// canonicalize applies the normalization required for a stable content
// digest (docs/PLAN.md Task 6 Steps §3): CRLF/CR -> LF, trim trailing
// whitespace per line, then Unicode NFC.
func canonicalize(raw []byte) []byte {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	return []byte(norm.NFC.String(s))
}

// Digest returns the canonical sha256 content digest of the document's
// original source, binding it per `plan_digest: sha256` in
// docs/foundry/docs/security/approval-and-provenance.md.
func (d *Document) Digest() [32]byte {
	return sha256.Sum256(canonicalize(d.raw))
}

// DigestHex returns Digest as a lowercase hex string.
func (d *Document) DigestHex() string {
	sum := d.Digest()
	return hex.EncodeToString(sum[:])
}
