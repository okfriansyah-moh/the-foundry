package provenance_test

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

func makeDoc() *plan.Document {
	return &plan.Document{ID: "plan-org-1", Title: "Org plan"}
}

// TestOrgValidate_ValidPath verifies all-green path.
func TestOrgValidate_ValidPath(t *testing.T) {
	content := []byte("hello world")
	digest := provenance.ComputeDigest(content)

	v := &provenance.OrgValidator{
		RequiredApproverRoles: []string{"engineering", "qa"},
		RefValidator:          provenance.DefaultRefValidator(),
	}
	src := provenance.OrgPlanSource{
		Repo:     "https://github.com/example/repo",
		Revision: "abc123",
		SourceDigests: []provenance.SourceRef{
			{Path: "README.md", Digest: digest},
		},
	}
	refs := []provenance.OrgRef{
		{Kind: "prd", URL: "https://docs.example.com/prd-123"},
	}
	approvers := []provenance.Approver{
		{Principal: "alice", Method: "role:engineering", At: time.Now()},
		{Principal: "bob", Method: "role:qa", At: time.Now()},
	}
	contentProvider := map[string][]byte{"README.md": content}

	result := v.Validate(makeDoc(), src, refs, approvers, contentProvider)
	if !result.Valid() {
		t.Errorf("expected valid; failed checks: %v", result.FailedChecks)
	}
}

// TestOrgValidate_TamperedDigest verifies tampered source digest rejection.
func TestOrgValidate_TamperedDigest(t *testing.T) {
	content := []byte("hello world")
	v := &provenance.OrgValidator{RefValidator: provenance.DefaultRefValidator()}
	src := provenance.OrgPlanSource{
		Repo:     "https://github.com/example/repo",
		Revision: "abc123",
		SourceDigests: []provenance.SourceRef{
			{Path: "README.md", Digest: "deadbeef0000"},
		},
	}
	result := v.Validate(makeDoc(), src, nil, nil, map[string][]byte{"README.md": content})
	if result.SourceOK {
		t.Error("SourceOK=true, want false for tampered digest")
	}
	if len(result.FailedChecks) == 0 {
		t.Error("FailedChecks empty, want at least one entry naming the mismatch")
	}
}

// TestOrgValidate_MissingQAApprover verifies missing role rejection.
func TestOrgValidate_MissingQAApprover(t *testing.T) {
	v := &provenance.OrgValidator{
		RequiredApproverRoles: []string{"engineering", "qa"},
		RefValidator:          provenance.DefaultRefValidator(),
	}
	// Only engineering approver present, qa missing.
	approvers := []provenance.Approver{
		{Principal: "alice", Method: "role:engineering", At: time.Now()},
	}
	result := v.Validate(makeDoc(), provenance.OrgPlanSource{Repo: "r", Revision: "sha"}, nil, approvers, nil)
	if result.ApproversOK {
		t.Error("ApproversOK=true, want false when QA role missing")
	}
	found := false
	for _, fc := range result.FailedChecks {
		if contains(fc, "qa") {
			found = true
		}
	}
	if !found {
		t.Errorf("FailedChecks does not name missing role 'qa': %v", result.FailedChecks)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// TestComputeDigest_Deterministic verifies digest is deterministic.
func TestComputeDigest_Deterministic(t *testing.T) {
	d1 := provenance.ComputeDigest([]byte("test"))
	d2 := provenance.ComputeDigest([]byte("test"))
	if d1 != d2 {
		t.Errorf("digest not deterministic: %q != %q", d1, d2)
	}
}
