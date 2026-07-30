package mission

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Target is mission-contract.md §1's `target:` block: the net-MRR goal a
// mission must reach, confirmed, before it is judged SUCCEEDED.
type Target struct {
	Metric                          string  `yaml:"metric" json:"metric"`
	Source                          string  `yaml:"source" json:"source"`
	Verification                    string  `yaml:"verification" json:"verification"`
	AmountUSD                       float64 `yaml:"amount_usd" json:"amount_usd"`
	ConfirmationWindow              string  `yaml:"confirmation_window" json:"confirmation_window"`
	MinimumUnrelatedPayingCustomers int     `yaml:"minimum_unrelated_paying_customers" json:"minimum_unrelated_paying_customers"`
	RefundChargebackRateBelow       float64 `yaml:"refund_chargeback_rate_below" json:"refund_chargeback_rate_below"`
}

// Budget is mission-contract.md §1's `budget:` block: the two envelopes a
// mission's evaluator checks (internal/ledger/cost.KindMissionMonthly and
// cost.KindExperiment respectively -- see workflow.go's checkBudget).
type Budget struct {
	MonthlyUSD         float64 `yaml:"monthly_usd" json:"monthly_usd"`
	TotalExperimentUSD float64 `yaml:"total_experiment_usd" json:"total_experiment_usd"`
}

// Cadence is mission-contract.md §1's `cadence:` block. Values are cadence
// words (e.g. "daily", "weekly") resolved to a time.Duration by
// parseCadence, or a raw Go duration string (e.g. "12h") as a fallback --
// a single source of truth for cadence-word validation lives in Go code,
// not duplicated into the JSONSchema (config/schemas/mission.schema.json
// deliberately only requires a non-empty string).
type Cadence struct {
	Observe string `yaml:"observe" json:"observe"`
	Improve string `yaml:"improve" json:"improve"`
}

// Constraints is mission-contract.md §1's `constraints:` block: the
// mechanical loop bounds Constitution C18 requires every mission to carry
// (never an open-ended loop).
type Constraints struct {
	MaximumActiveProducts   int `yaml:"maximum_active_products" json:"maximum_active_products"`
	MaximumValidationCycles int `yaml:"maximum_validation_cycles" json:"maximum_validation_cycles"`
	MaximumNoProgressCycles int `yaml:"maximum_no_progress_cycles" json:"maximum_no_progress_cycles"`
}

// Pause/terminate condition literals, exactly as mission-contract.md §1's
// `pause_when:`/`terminate_when:` lists enumerate them.
const (
	PauseMonthlyBudgetExhausted = "monthly-budget-exhausted"
	PausePaymentDataUnavailable = "payment-data-unavailable"
	PauseUnforeseenHumanGate    = "unforeseen-human-gate"

	TerminateTotalBudgetExhausted = "total-budget-exhausted"
	TerminateProhibitedMarket     = "prohibited-market-detected"
	TerminateNoViableCandidate    = "no-viable-candidate-after-max-cycles"
)

// Post-success-policy literals, exactly as mission-contract.md §1 lists
// them ("stop | maintenance | raise-target | continue-growth |
// start-another-product").
const (
	PostSuccessStop                = "stop"
	PostSuccessMaintenance         = "maintenance"
	PostSuccessRaiseTarget         = "raise-target"
	PostSuccessContinueGrowth      = "continue-growth"
	PostSuccessStartAnotherProduct = "start-another-product"
)

// Contract is the typed shape of mission-contract.md §1's MissionContract,
// implemented field-for-field against the governing doc -- nothing here is
// improvised beyond it.
type Contract struct {
	ID                string      `yaml:"id" json:"id"`
	Statement         string      `yaml:"statement" json:"statement"`
	Target            Target      `yaml:"target" json:"target"`
	Budget            Budget      `yaml:"budget" json:"budget"`
	Cadence           Cadence     `yaml:"cadence" json:"cadence"`
	Constraints       Constraints `yaml:"constraints" json:"constraints"`
	PauseWhen         []string    `yaml:"pause_when" json:"pause_when"`
	TerminateWhen     []string    `yaml:"terminate_when" json:"terminate_when"`
	PostSuccessPolicy string      `yaml:"post_success_policy" json:"post_success_policy"`
	// RequiresBuildVerdict is set for the personal-venture profile (docs/PLAN.md
	// Task 102 / OPP-03, Constitution C23): an unattended mission with this flag
	// set may not start without a reproducible BUILD verdict over stored
	// opportunity evidence. It is optional and defaults to false, preserving
	// the organization/10x path unchanged.
	RequiresBuildVerdict bool `yaml:"requires_build_verdict,omitempty" json:"requires_build_verdict,omitempty"`
}

// document is the on-disk YAML/JSON shape: a single top-level "mission"
// key wrapping Contract, exactly as mission-contract.md §1 shows it.
type document struct {
	Mission Contract `yaml:"mission" json:"mission"`
}

// ContractError describes one JSONSchema violation in a mission contract,
// with a JSON-pointer-style path to the offending field (mirrors
// internal/profile's ConfigError).
type ContractError struct {
	Path    string
	Message string
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ParseYAML parses raw as a mission-contract.md §1 document, validates it
// against config/schemas/mission.schema.json, and returns the typed
// Contract. Schema validation runs against the JSON projection of the
// parsed YAML (the same not-YAML-native approach internal/profile's
// ValidateConfig uses), so a schema violation is reported before any
// caller ever sees a Contract built from invalid input.
func ParseYAML(raw []byte) (Contract, error) {
	var generic interface{}
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return Contract{}, fmt.Errorf("mission: parse yaml: %w", err)
	}
	generic = normalizeYAML(generic)

	if err := validateDocument(generic); err != nil {
		return Contract{}, err
	}

	var doc document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Contract{}, fmt.Errorf("mission: decode contract: %w", err)
	}
	return doc.Mission, nil
}

// normalizeYAML converts yaml.v3's map[string]interface{} (and any nested
// map[interface{}]interface{} a legacy decode path might produce) into the
// map[string]interface{}/[]interface{}/string/float64/bool/nil shapes
// encoding/json and the jsonschema validator require. yaml.v3 already
// decodes generic YAML into these JSON-compatible shapes directly for
// string-keyed maps, so this is a defensive no-op in the common case, not
// a real transformation.
func normalizeYAML(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, vv := range val {
			out[k] = normalizeYAML(vv)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, vv := range val {
			out[i] = normalizeYAML(vv)
		}
		return out
	default:
		return val
	}
}

// toJSONInstance round-trips generic (a normalizeYAML'd value) through
// encoding/json so validateDocument always validates the same instance
// shape internal/profile's ValidateConfig does (float64/string/bool/map/
// slice/nil), regardless of any residual yaml.v3-specific type.
func toJSONInstance(generic interface{}) (interface{}, error) {
	raw, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("mission: encode parsed yaml as json: %w", err)
	}
	var instance interface{}
	if err := json.Unmarshal(raw, &instance); err != nil {
		return nil, fmt.Errorf("mission: decode json instance: %w", err)
	}
	return instance, nil
}

// parseDuration parses a mission-contract.md duration string. Go's
// time.ParseDuration has no "d" (day) unit, but mission-contract.md's own
// example uses "30d" for confirmation_window -- this adds a single "Nd"
// day-suffix case on top of time.ParseDuration's own h/m/s units, rather
// than hand-rolling a full duration grammar.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("mission: invalid day duration %q: %w", s, err)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("mission: invalid duration %q: %w", s, err)
	}
	return d, nil
}

// cadenceWords maps mission-contract.md's cadence vocabulary to a
// concrete interval. Any value not in this table falls back to
// parseDuration (e.g. an operator-supplied "12h" override) -- decision
// (no-gaps rule): the doc names only "daily"/"weekly" but does not close
// the vocabulary, so the smallest reversible option is a fixed table for
// the documented words plus a raw-duration escape hatch, not a rejection
// of anything else.
var cadenceWords = map[string]time.Duration{
	"hourly":  time.Hour,
	"daily":   24 * time.Hour,
	"weekly":  7 * 24 * time.Hour,
	"monthly": 30 * 24 * time.Hour,
}

// parseCadence resolves a Cadence field (e.g. "daily") to a
// time.Duration.
func parseCadence(word string) (time.Duration, error) {
	if d, ok := cadenceWords[strings.ToLower(strings.TrimSpace(word))]; ok {
		return d, nil
	}
	return parseDuration(word)
}
