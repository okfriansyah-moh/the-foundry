package compiler

// OrgGovernancePack holds the governance extension fields declared by the
// org layer (Task 54 / TX-01). These are read from the `org_governance:`
// stanza of the org profile YAML and enforced by the policy PDP for
// org-scoped actions (approve, push authorization).
//
// The core policy fields (risk_tier_controls, executor_allowlist, etc.) are
// handled by the existing Compile/LayerPolicy path. OrgGovernancePack is a
// complementary, org-layer-only structure for governance rules that do not
// map 1:1 onto the tighten-only merge algorithm.
type OrgGovernancePack struct {
	// RequiredApproverRoles names the roles whose approval is required for
	// org-scoped plan admission. Matched against Approvers' roles per Task 25.
	RequiredApproverRoles []string `yaml:"required_approver_roles"`

	// PushAuthorization is the push-authority policy for shared org branches.
	// The only valid value for 10x org profiles is "kernel-only" — any other
	// value is rejected at compile time (C4 enforcement: only go-kernel may
	// write to shared branches).
	PushAuthorization string `yaml:"push_authorization"`
}

// AllowsPushBy reports whether the given principal is authorized to push to
// shared org branches under this governance pack.
// "kernel-only" mode permits only "service:go-kernel".
func (g OrgGovernancePack) AllowsPushBy(principal string) bool {
	switch g.PushAuthorization {
	case "kernel-only":
		return principal == "service:go-kernel"
	default:
		// Unknown mode: deny by default (fail-closed per C4).
		return false
	}
}

// ValidateOrgGovernancePack returns an error if the pack contains invalid
// or unsupported values. Called by the org-layer compiler path.
func ValidateOrgGovernancePack(g OrgGovernancePack) error {
	switch g.PushAuthorization {
	case "kernel-only", "":
		// empty = unset = no org-level restriction; kernel-only = C4 enforcement.
	default:
		return &CompileError{
			Layer:   LayerOrg,
			Field:   "push_authorization",
			Message: "unknown value " + g.PushAuthorization + "; only \"kernel-only\" is accepted",
		}
	}
	return nil
}
