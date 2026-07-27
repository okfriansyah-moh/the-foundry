package provenance

import (
	"crypto/ed25519"
	"fmt"
)

// Revoke marks a as revoked, records who revoked it and why, and re-signs
// a under priv so the revocation is itself part of the signed,
// tamper-evident artifact (docs/foundry/docs/security/approval-and-provenance.md
// §1's `revocation: {revoked, revoked_by, reason}` field). Callers that
// hold a *Store should use Store.Revoke instead, which also persists the
// result — this lower-level function exists so revocation and signing
// stay in the same package as Sign/Verify, with no other package ever
// setting the unexported revoked/revokedBy/revocationReason fields
// directly.
func Revoke(priv ed25519.PrivateKey, a *ApprovedPlan, revokedBy, reason string) error {
	if reason == "" {
		return fmt.Errorf("provenance: revoke requires a non-empty reason")
	}
	if revokedBy == "" {
		return fmt.Errorf("provenance: revoke requires a non-empty revokedBy principal")
	}
	a.revoked = true
	a.revokedBy = revokedBy
	a.revocationReason = reason
	return Sign(priv, a)
}
