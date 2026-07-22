package provenance

import (
	"context"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// VerifyResult is the outcome of VerifyPlanFile: whether the on-disk plan
// file still matches the digest its approval is bound to, plus the
// granted-subset-of-requested proof (docs/PLAN.md Task 8 Step 4).
type VerifyResult struct {
	PlanID         string
	FileDigest     string
	ApprovedDigest string
	DigestMatches  bool
	Requested      []plan.Permission
	Granted        []plan.Permission
	GrantedSubset  bool
}

// VerifyPlanFile recomputes the digest of the plan file at path,
// loads the ApprovedPlan for planID via store (which itself verifies the
// signature — a tampered row errors out of Load before this function ever
// sees it), and checks that the recomputed digest still matches the
// approved digest and that granted ⊆ requested.
func VerifyPlanFile(ctx context.Context, store *Store, planID, path string) (*VerifyResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provenance: read plan file %s: %w", path, err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("provenance: parse plan file %s: %w", path, err)
	}

	approved, err := store.Load(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("provenance: load approved plan %s: %w", planID, err)
	}

	fileDigest := doc.DigestHex()
	requested := approved.Requested()
	granted := approved.Granted()

	return &VerifyResult{
		PlanID:         planID,
		FileDigest:     fileDigest,
		ApprovedDigest: approved.PlanDigest(),
		DigestMatches:  fileDigest == approved.PlanDigest(),
		Requested:      requested,
		Granted:        granted,
		GrantedSubset:  isSubset(granted, requested),
	}, nil
}

// isSubset reports whether every permission in granted also appears in
// requested.
func isSubset(granted, requested []plan.Permission) bool {
	set := make(map[plan.Permission]struct{}, len(requested))
	for _, r := range requested {
		set[r] = struct{}{}
	}
	for _, g := range granted {
		if _, ok := set[g]; !ok {
			return false
		}
	}
	return true
}
