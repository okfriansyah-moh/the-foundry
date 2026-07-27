package sandbox

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EgressRule is one destination the sandbox's egress gate is permitted to
// relay traffic to. Host must be an exact, fully-qualified hostname — no
// wildcards, no CIDR, no "*" — because the whole point of this allowlist is
// staying narrow (docs/PLAN.md Task 34 Steps: "nothing else by default").
type EgressRule struct {
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"`
	Reason string `yaml:"reason"`
}

// allowlistFile is the on-disk shape of config/sandbox-egress-allowlist.yaml.
type allowlistFile struct {
	Version int          `yaml:"version"`
	Allow   []EgressRule `yaml:"allow"`
}

// EgressAllowlist is the policy the gate proxy (gate/main.go) checks every
// CONNECT request's target against, and the policy oci.go's escape/legit
// tests assert the topology actually enforces.
type EgressAllowlist struct {
	Version int
	Allow   []EgressRule
}

// LoadEgressAllowlist reads and validates an EgressAllowlist from a
// config/sandbox-egress-allowlist.yaml-shaped file.
func LoadEgressAllowlist(path string) (EgressAllowlist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return EgressAllowlist{}, fmt.Errorf("sandbox: read allowlist %s: %w", path, err)
	}
	var f allowlistFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return EgressAllowlist{}, fmt.Errorf("sandbox: parse allowlist %s: %w", path, err)
	}
	a := EgressAllowlist{Version: f.Version, Allow: f.Allow}
	if err := a.Validate(); err != nil {
		return EgressAllowlist{}, fmt.Errorf("sandbox: invalid allowlist %s: %w", path, err)
	}
	return a, nil
}

// Validate rejects any rule that would widen the allowlist past "exact host,
// exact port" — a wildcard or empty host, or a port outside 1-65535 — since
// a denylist-shaped or wildcard entry here would defeat the default-deny
// model this package exists to enforce (A01/SSRF, security-hardening
// SKILL.md: "an allowlist, not a denylist").
func (a EgressAllowlist) Validate() error {
	if a.Version != 1 {
		return fmt.Errorf("sandbox: unsupported allowlist version %d (want 1)", a.Version)
	}
	seen := make(map[string]bool, len(a.Allow))
	for i, r := range a.Allow {
		host := strings.TrimSpace(r.Host)
		if host == "" {
			return fmt.Errorf("sandbox: allow[%d]: empty host", i)
		}
		if strings.ContainsAny(host, "*?") {
			return fmt.Errorf("sandbox: allow[%d]: wildcard host %q not permitted", i, r.Host)
		}
		if r.Port < 1 || r.Port > 65535 {
			return fmt.Errorf("sandbox: allow[%d]: port %d out of range", i, r.Port)
		}
		key := fmt.Sprintf("%s:%d", strings.ToLower(host), r.Port)
		if seen[key] {
			return fmt.Errorf("sandbox: allow[%d]: duplicate entry %s", i, key)
		}
		seen[key] = true
	}
	return nil
}

// Allows reports whether host:port is covered by an exact-match allowlist
// entry (case-insensitive host comparison; DNS hostnames are not
// case-sensitive).
func (a EgressAllowlist) Allows(host string, port int) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, r := range a.Allow {
		if strings.ToLower(r.Host) == host && r.Port == port {
			return true
		}
	}
	return false
}
