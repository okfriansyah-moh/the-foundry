package capability

import "time"

// Availability enumerates the deployment states a Record may declare. It is
// a closed set (unlike Features) because routing must treat it as a hard
// gate: only Supported executors are ever Eligible.
const (
	// AvailabilitySupported means an adapter exists and passes the shared
	// contract suite; only these records are ever returned by Eligible.
	AvailabilitySupported = "supported"
	// AvailabilityUnsupported means the provider has a registry row for
	// documentation/decision-trail purposes but no working adapter yet
	// (e.g. Kimi/Kilo stubs, docs/PLAN.md Task 89). Never Eligible.
	AvailabilityUnsupported = "unsupported"
)

// Sandbox eligibility (docs/PLAN.md Task 142 / SEC-05). Exactly one state per
// record; missing/unknown refuses load.
const (
	SandboxSupported   = "sandbox_supported"
	SandboxHostOnly    = "host_only"
	SandboxUnsupported = "unsupported"
)

// Record is one executor provider's declared capabilities. It is pure data
// mirroring provider-execution-classes.md §6.7's capability contract.
type Record struct {
	// Provider is the executor's registry name, matching the string it is
	// registered under in internal/executor (e.g. "claude-code").
	Provider string `yaml:"provider"`
	// ExecutionClass is the provider-execution-classes.md class this
	// provider belongs to (e.g. "cli-agentic", "api"). Free-form string —
	// the six named classes live in the docs, not an enum here, so a new
	// class needs no code change.
	ExecutionClass string `yaml:"execution_class"`
	// Features is the OPEN set of capability strings this provider offers,
	// drawn from §6.7's vocabulary. New features need no Go change.
	Features []string `yaml:"features"`
	// Availability is one of the AvailabilityX constants. Only
	// AvailabilitySupported records are Eligible.
	Availability string `yaml:"availability"`
	// ProfileAllow, when non-empty, restricts this provider to exactly the
	// listed profile IDs. Empty means "no allow restriction".
	ProfileAllow []string `yaml:"profile_allow"`
	// ProfileDeny lists profile IDs this provider must never serve. Deny
	// always wins over allow.
	ProfileDeny []string `yaml:"profile_deny"`
	// LastVerifiedAt is when this record's capabilities were last confirmed
	// against the real provider. Records older than StalenessLimit fail
	// `make fitness` (docs/PLAN.md Task 84 staleness lint).
	LastVerifiedAt time.Time `yaml:"last_verified_at"`
	// RequiresSandbox declares whether this executor MUST run inside the
	// mandatory executor sandbox (docs/PLAN.md Task 115 / SEC-01). It
	// defaults to true (nil == true): an autonomous coding executor is
	// sandboxed unless it explicitly opts out. An executor that sets it false
	// must name SandboxOptOutReason and is refused for any profile whose
	// policy demands sandboxing.
	RequiresSandbox *bool `yaml:"requires_sandbox,omitempty"`
	// SandboxOptOutReason is the mandatory justification when RequiresSandbox
	// is explicitly false.
	SandboxOptOutReason string `yaml:"sandbox_optout_reason,omitempty"`
	// SandboxEligibility is docs/PLAN.md Task 142: exactly one of
	// sandbox_supported | host_only | unsupported. Missing/unknown refuses
	// load. Only sandbox_supported may match require_sandbox=true.
	SandboxEligibility string `yaml:"sandbox_eligibility"`
}

// SandboxRequired reports whether this executor must run inside the sandbox.
// The default (unset) is true — sandboxing is opt-out, not opt-in (C24).
func (r Record) SandboxRequired() bool {
	return r.RequiresSandbox == nil || *r.RequiresSandbox
}

// IsSandboxSupported reports Task 142 eligibility for autonomous sandbox runs.
func (r Record) IsSandboxSupported() bool {
	return r.SandboxEligibility == SandboxSupported
}

// StalenessLimit is how old a Record.LastVerifiedAt may be before the
// fitness staleness lint fails it by name. 180 days per Task 84's card.
const StalenessLimit = 180 * 24 * time.Hour

// Registry is the loaded, validated set of executor capability records.
type Registry struct {
	Executors []Record `yaml:"executors"`
}
