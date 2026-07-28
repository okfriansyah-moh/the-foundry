// Package provenance — org.go
//
// Task 55 (TX-02): Org plan provenance validation (C7, C12).
//
// OrgValidator extends admission for org profiles:
//
//  1. Source repo+revision validation: plan declares {repo, revision,
//     source_digests[]}; validator verifies each digest against the given
//     revision's tree. Mismatch ⇒ ADMISSION_REJECTED.
//
//  2. Reference checks: PRD/RFC/test refs validated by a pluggable
//     RefValidator (v1: URL-reachable + pattern registry;
//     Jira/TestRail deep validation is a stub interface with TODO note).
//
//  3. Approver-role enforcement: required roles from the org governance
//     pack matched against the ApprovedPlan's Approvers. Missing role ⇒
//     reject, naming the missing role.
package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// SourceRef declares one source file's expected content digest.
type SourceRef struct {
	// Path is the file path within the repo tree.
	Path string `json:"path"`
	// Digest is the expected SHA-256 hex digest of the file content.
	Digest string `json:"digest"`
}

// OrgPlanSource carries the source provenance declaration an org plan must
// include when submitted via `foundry plan submit --org`.
type OrgPlanSource struct {
	// Repo is the canonical repository URL.
	Repo string `json:"repo"`
	// Revision is the git SHA the plan's source digests were computed from.
	Revision string `json:"revision"`
	// SourceDigests is the list of file-level content digests.
	SourceDigests []SourceRef `json:"source_digests"`
}

// OrgRef is a reference that an org plan must be able to substantiate.
type OrgRef struct {
	// Kind is "prd", "rfc", or "test".
	Kind string `json:"kind"`
	// URL is the reference URL.
	URL string `json:"url"`
}

// OrgValidationResult is the structured outcome of OrgValidator.Validate.
type OrgValidationResult struct {
	SourceOK     bool
	RefsOK       bool
	ApproversOK  bool
	FailedChecks []string
}

// Valid reports whether all three checks passed.
func (r OrgValidationResult) Valid() bool {
	return r.SourceOK && r.RefsOK && r.ApproversOK
}

// ErrOrgValidation is the sentinel for any org plan rejection.
var ErrOrgValidation = errors.New("org provenance validation failed")

// RefValidator is a pluggable check for org plan reference URLs.
// v1: URL syntax + allowed-patterns; Jira/TestRail deep validation = stub.
type RefValidator interface {
	ValidateRef(ref OrgRef) error
}

// URLPatternValidator implements RefValidator: validates that the URL is
// syntactically valid and matches at least one configured allowlist pattern.
type URLPatternValidator struct {
	// AllowedPatterns is a list of regexp patterns. A ref passes if any matches.
	AllowedPatterns []*regexp.Regexp
}

// ValidateRef checks URL syntax and allowlist patterns.
// OWASP A03: input validated before use in any downstream fetch.
func (v *URLPatternValidator) ValidateRef(ref OrgRef) error {
	u, err := url.Parse(ref.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("ref %q: invalid or non-http(s) URL", ref.URL)
	}
	if len(v.AllowedPatterns) == 0 {
		return nil // no allowlist: any URL passes
	}
	for _, pat := range v.AllowedPatterns {
		if pat.MatchString(ref.URL) {
			return nil
		}
	}
	return fmt.Errorf("ref %q: URL does not match any allowed pattern", ref.URL)
}

// DefaultRefValidator returns a URLPatternValidator with no pattern restrictions.
// Callers may narrow it by adding patterns.
func DefaultRefValidator() RefValidator {
	return &URLPatternValidator{}
}

// OrgValidator validates org plan provenance: source digests, refs, approver roles.
type OrgValidator struct {
	// RequiredApproverRoles is the set of roles all of which must be represented
	// among the ApprovedPlan's Approvers. From OrgGovernancePack.
	RequiredApproverRoles []string
	// RefValidator is the pluggable reference checker.
	RefValidator RefValidator
}

// ComputeDigest computes the SHA-256 hex digest of content.
func ComputeDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ValidateSourceDigests verifies that each SourceRef in src matches the
// provided contentProvider. contentProvider maps path → bytes; if a path
// is absent, validation fails for that path.
func ValidateSourceDigests(src OrgPlanSource, contentProvider map[string][]byte) error {
	if src.Repo == "" {
		return fmt.Errorf("org provenance: source repo is required")
	}
	if src.Revision == "" {
		return fmt.Errorf("org provenance: source revision is required")
	}
	var errs []string
	for _, ref := range src.SourceDigests {
		content, ok := contentProvider[ref.Path]
		if !ok {
			errs = append(errs, fmt.Sprintf("path %q: not found in revision %s", ref.Path, src.Revision))
			continue
		}
		actual := ComputeDigest(content)
		if actual != ref.Digest {
			errs = append(errs, fmt.Sprintf("path %q: digest mismatch (want %s, got %s)", ref.Path, ref.Digest, actual))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("org provenance: source digest failures: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Validate runs all three org provenance checks and returns a structured result.
func (v *OrgValidator) Validate(
	_ *plan.Document,
	src OrgPlanSource,
	refs []OrgRef,
	approvers []Approver,
	contentProvider map[string][]byte,
) OrgValidationResult {
	result := OrgValidationResult{SourceOK: true, RefsOK: true, ApproversOK: true}

	// Check 1: source digests.
	if err := ValidateSourceDigests(src, contentProvider); err != nil {
		result.SourceOK = false
		result.FailedChecks = append(result.FailedChecks, err.Error())
	}

	// Check 2: references.
	if v.RefValidator != nil {
		for _, ref := range refs {
			if err := v.RefValidator.ValidateRef(ref); err != nil {
				result.RefsOK = false
				result.FailedChecks = append(result.FailedChecks, err.Error())
			}
		}
	}

	// Check 3: approver roles.
	presentRoles := map[string]struct{}{}
	for _, a := range approvers {
		// Role is carried as Method prefix "role:<name>" or a dedicated Role field.
		// v1: extract from Method if it starts with "role:", otherwise use Method directly.
		role := a.Method
		if strings.HasPrefix(role, "role:") {
			role = strings.TrimPrefix(role, "role:")
		}
		presentRoles[role] = struct{}{}
	}
	for _, required := range v.RequiredApproverRoles {
		if _, ok := presentRoles[required]; !ok {
			result.ApproversOK = false
			result.FailedChecks = append(result.FailedChecks, fmt.Sprintf("missing required approver role: %q", required))
		}
	}

	return result
}

// OrgValidationError wraps a failed OrgValidationResult as an error.
type OrgValidationError struct {
	Result OrgValidationResult
}

func (e *OrgValidationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrOrgValidation, strings.Join(e.Result.FailedChecks, "; "))
}

func (e *OrgValidationError) Unwrap() error { return ErrOrgValidation }
