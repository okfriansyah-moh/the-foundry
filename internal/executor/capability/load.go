package capability

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads and strictly validates the capability registry at path. Unknown
// top-level or per-record YAML keys are rejected (KnownFields), so a typo or
// an out-of-schema field is a load error, not a silently-ignored value. Each
// record is validated for the required fields routing depends on.
func Load(path string) (Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, fmt.Errorf("capability: read %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var reg Registry
	if err := dec.Decode(&reg); err != nil {
		return Registry{}, fmt.Errorf("capability: parse %s: %w", path, err)
	}

	if err := reg.validate(); err != nil {
		return Registry{}, fmt.Errorf("capability: %s: %w", path, err)
	}
	return reg, nil
}

// validate enforces per-record invariants a plain unmarshal cannot express:
// required fields present, availability a known value, no duplicate provider
// names (which would make Eligible's result depend on file order for the
// deny/allow of a shadowed row).
func (r Registry) validate() error {
	seen := map[string]bool{}
	for i, rec := range r.Executors {
		if rec.Provider == "" {
			return fmt.Errorf("executors[%d]: missing provider", i)
		}
		if seen[rec.Provider] {
			return fmt.Errorf("executors[%d]: duplicate provider %q", i, rec.Provider)
		}
		seen[rec.Provider] = true
		if rec.ExecutionClass == "" {
			return fmt.Errorf("provider %q: missing execution_class", rec.Provider)
		}
		switch rec.Availability {
		case AvailabilitySupported, AvailabilityUnsupported:
		default:
			return fmt.Errorf("provider %q: availability must be %q or %q, got %q",
				rec.Provider, AvailabilitySupported, AvailabilityUnsupported, rec.Availability)
		}
		if rec.LastVerifiedAt.IsZero() {
			return fmt.Errorf("provider %q: missing last_verified_at", rec.Provider)
		}
		// Task 115: a sandbox opt-out must be justified — the default is to
		// sandbox, and silently disabling it is exactly the fail-open this
		// card closes.
		if rec.RequiresSandbox != nil && !*rec.RequiresSandbox && rec.SandboxOptOutReason == "" {
			return fmt.Errorf("provider %q: requires_sandbox=false must name a sandbox_optout_reason", rec.Provider)
		}
	}
	return nil
}

// Stale returns the providers whose LastVerifiedAt is more than StalenessLimit
// before now, in deterministic (registry) order. The fitness staleness lint
// (docs/PLAN.md Task 84) fails when this is non-empty.
func (r Registry) Stale(now time.Time) []string {
	var stale []string
	for _, rec := range r.Executors {
		if now.Sub(rec.LastVerifiedAt) > StalenessLimit {
			stale = append(stale, rec.Provider)
		}
	}
	return stale
}

// Lookup returns the record for provider, or false if absent.
func (r Registry) Lookup(provider string) (Record, bool) {
	for _, rec := range r.Executors {
		if rec.Provider == provider {
			return rec, true
		}
	}
	return Record{}, false
}
