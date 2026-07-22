package admission

import (
	"encoding/json"
	"fmt"
)

// Tier is an admission tier as defined in
// docs/foundry/docs/autonomy/admission-tiers.md §2. Values are ordered by
// increasing restriction: TierA0 is the least restrictive (fully automatic),
// TierH the most (human authorization required). Ruleset evaluation picks
// the highest (most restrictive) tier among all fired rules — "highest
// floor wins".
type Tier int

const (
	// TierA0 is fully automatic: low-risk, reversible changes.
	TierA0 Tier = iota
	// TierA1 is automatic after deterministic verification and a
	// synthetic/canary gate.
	TierA1
	// TierA2 is automatic only inside an explicitly pre-authorized
	// personal profile.
	TierA2
	// TierH requires human authorization.
	TierH
)

// String returns the canonical tier label used in persisted decisions and
// governing-doc examples ("A0" | "A1" | "A2" | "H").
func (t Tier) String() string {
	switch t {
	case TierA0:
		return "A0"
	case TierA1:
		return "A1"
	case TierA2:
		return "A2"
	case TierH:
		return "H"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// MarshalJSON encodes the tier as its canonical string label.
func (t Tier) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON decodes a canonical tier string label.
func (t *Tier) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("admission: unmarshal tier: %w", err)
	}
	switch s {
	case "A0":
		*t = TierA0
	case "A1":
		*t = TierA1
	case "A2":
		*t = TierA2
	case "H":
		*t = TierH
	default:
		return fmt.Errorf("admission: unknown tier label %q", s)
	}
	return nil
}
