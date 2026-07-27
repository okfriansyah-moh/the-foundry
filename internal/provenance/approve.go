package provenance

import (
	"crypto/ed25519"
	"fmt"
)

// AppendApprover records one additional approval on a and re-signs a under
// priv, so the new approver is itself part of the signed, tamper-evident
// artifact rather than a side-channel record (docs/PLAN.md Task 25 /
// Constitution C12; mirrors Revoke's mutate-then-resign pattern in
// revoke.go). Callers that hold a *Store should use Store.AddApprover
// instead, which also persists the result.
//
// This never mutates approver.At — callers set it explicitly so the
// recorded approval time reflects when the strong-auth assertion was
// verified, not when it happened to be persisted.
func AppendApprover(priv ed25519.PrivateKey, a *ApprovedPlan, approver Approver) error {
	if approver.Principal == "" {
		return fmt.Errorf("provenance: append approver requires a non-empty principal")
	}
	if approver.Method == "" {
		return fmt.Errorf("provenance: append approver requires a non-empty method")
	}
	a.approvers = append(a.approvers, approver)
	return Sign(priv, a)
}
