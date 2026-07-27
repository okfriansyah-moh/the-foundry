package mission

import (
	"strings"
	"testing"
	"time"
)

// usd100ExampleYAML instantiates docs/foundry/docs/autonomy/mission-
// contract.md §1's illustrative MissionContract example. decision (no-gaps
// rule): the doc's own example writes
// `refund_chargeback_rate_below: configured` -- a prose placeholder
// meaning "set from the profile's policy defaults", not a literal YAML
// value the schema could accept (it is not a number). This fixture
// substitutes a concrete value (0.05 == 5%) for that placeholder so the
// contract can actually be parsed/round-tripped; every other field is
// copied from the doc verbatim.
const usd100ExampleYAML = `
mission:
  id: 11111111-1111-1111-1111-111111111111
  statement: "Reach at least USD 100 in verified net monthly recurring revenue."
  target:
    metric: net_mrr
    source: payment-provider-ledger
    verification: reconciled
    amount_usd: 100
    confirmation_window: 30d
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: 0.05
  budget:
    monthly_usd: 100
    total_experiment_usd: 500
  cadence:
    observe: daily
    improve: weekly
  constraints:
    maximum_active_products: 1
    maximum_validation_cycles: 12
    maximum_no_progress_cycles: 4
  pause_when:
    - monthly-budget-exhausted
    - payment-data-unavailable
    - unforeseen-human-gate
  terminate_when:
    - total-budget-exhausted
    - prohibited-market-detected
    - no-viable-candidate-after-max-cycles
  post_success_policy: stop
`

func TestParseYAML_USD100Example(t *testing.T) {
	c, err := ParseYAML([]byte(usd100ExampleYAML))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	if c.ID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ID = %q", c.ID)
	}
	if c.Target.Metric != "net_mrr" || c.Target.Source != "payment-provider-ledger" || c.Target.Verification != "reconciled" {
		t.Errorf("target = %+v", c.Target)
	}
	if c.Target.AmountUSD != 100 {
		t.Errorf("Target.AmountUSD = %v, want 100", c.Target.AmountUSD)
	}
	if c.Target.MinimumUnrelatedPayingCustomers != 3 {
		t.Errorf("MinimumUnrelatedPayingCustomers = %d, want 3", c.Target.MinimumUnrelatedPayingCustomers)
	}
	if c.Budget.MonthlyUSD != 100 || c.Budget.TotalExperimentUSD != 500 {
		t.Errorf("budget = %+v", c.Budget)
	}
	if c.Cadence.Observe != "daily" || c.Cadence.Improve != "weekly" {
		t.Errorf("cadence = %+v", c.Cadence)
	}
	if c.Constraints.MaximumActiveProducts != 1 || c.Constraints.MaximumValidationCycles != 12 || c.Constraints.MaximumNoProgressCycles != 4 {
		t.Errorf("constraints = %+v", c.Constraints)
	}
	wantPause := []string{PauseMonthlyBudgetExhausted, PausePaymentDataUnavailable, PauseUnforeseenHumanGate}
	if strings.Join(c.PauseWhen, ",") != strings.Join(wantPause, ",") {
		t.Errorf("pause_when = %v, want %v", c.PauseWhen, wantPause)
	}
	wantTerminate := []string{TerminateTotalBudgetExhausted, TerminateProhibitedMarket, TerminateNoViableCandidate}
	if strings.Join(c.TerminateWhen, ",") != strings.Join(wantTerminate, ",") {
		t.Errorf("terminate_when = %v, want %v", c.TerminateWhen, wantTerminate)
	}
	if c.PostSuccessPolicy != PostSuccessStop {
		t.Errorf("post_success_policy = %q, want %q", c.PostSuccessPolicy, PostSuccessStop)
	}

	window, err := parseDuration(c.Target.ConfirmationWindow)
	if err != nil {
		t.Fatalf("parseDuration(%q): %v", c.Target.ConfirmationWindow, err)
	}
	if window != 30*24*time.Hour {
		t.Errorf("confirmation window = %v, want 720h", window)
	}
}

func TestParseYAML_Invalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing required field (budget)",
			yaml: `
mission:
  id: m1
  statement: "test"
  target:
    metric: net_mrr
    source: payment-provider-ledger
    verification: reconciled
    amount_usd: 100
    confirmation_window: 30d
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: 0.05
  cadence:
    observe: daily
    improve: weekly
  constraints:
    maximum_active_products: 1
    maximum_validation_cycles: 12
    maximum_no_progress_cycles: 4
  pause_when: [monthly-budget-exhausted]
  terminate_when: [total-budget-exhausted]
  post_success_policy: stop
`,
		},
		{
			name: "wrong metric const",
			yaml: `
mission:
  id: m1
  statement: "test"
  target:
    metric: gross_revenue
    source: payment-provider-ledger
    verification: reconciled
    amount_usd: 100
    confirmation_window: 30d
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: 0.05
  budget: {monthly_usd: 100, total_experiment_usd: 500}
  cadence: {observe: daily, improve: weekly}
  constraints: {maximum_active_products: 1, maximum_validation_cycles: 12, maximum_no_progress_cycles: 4}
  pause_when: [monthly-budget-exhausted]
  terminate_when: [total-budget-exhausted]
  post_success_policy: stop
`,
		},
		{
			name: "unknown pause_when value",
			yaml: `
mission:
  id: m1
  statement: "test"
  target:
    metric: net_mrr
    source: payment-provider-ledger
    verification: reconciled
    amount_usd: 100
    confirmation_window: 30d
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: 0.05
  budget: {monthly_usd: 100, total_experiment_usd: 500}
  cadence: {observe: daily, improve: weekly}
  constraints: {maximum_active_products: 1, maximum_validation_cycles: 12, maximum_no_progress_cycles: 4}
  pause_when: [some-made-up-reason]
  terminate_when: [total-budget-exhausted]
  post_success_policy: stop
`,
		},
		{
			name: "invalid post_success_policy",
			yaml: `
mission:
  id: m1
  statement: "test"
  target:
    metric: net_mrr
    source: payment-provider-ledger
    verification: reconciled
    amount_usd: 100
    confirmation_window: 30d
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: 0.05
  budget: {monthly_usd: 100, total_experiment_usd: 500}
  cadence: {observe: daily, improve: weekly}
  constraints: {maximum_active_products: 1, maximum_validation_cycles: 12, maximum_no_progress_cycles: 4}
  pause_when: [monthly-budget-exhausted]
  terminate_when: [total-budget-exhausted]
  post_success_policy: never-stop
`,
		},
		{
			name: "unknown top-level field rejected",
			yaml: `
mission:
  id: m1
  statement: "test"
  target:
    metric: net_mrr
    source: payment-provider-ledger
    verification: reconciled
    amount_usd: 100
    confirmation_window: 30d
    minimum_unrelated_paying_customers: 3
    refund_chargeback_rate_below: 0.05
  budget: {monthly_usd: 100, total_experiment_usd: 500}
  cadence: {observe: daily, improve: weekly}
  constraints: {maximum_active_products: 1, maximum_validation_cycles: 12, maximum_no_progress_cycles: 4}
  pause_when: [monthly-budget-exhausted]
  terminate_when: [total-budget-exhausted]
  post_success_policy: stop
  unexpected_field: true
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseYAML([]byte(tt.yaml)); err == nil {
				t.Fatal("ParseYAML: want error, got nil")
			}
		})
	}
}

func TestParseCadence(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"daily", 24 * time.Hour},
		{"weekly", 7 * 24 * time.Hour},
		{"hourly", time.Hour},
		{"monthly", 30 * 24 * time.Hour},
		{"12h", 12 * time.Hour},
	}
	for _, tt := range tests {
		got, err := parseCadence(tt.in)
		if err != nil {
			t.Fatalf("parseCadence(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseCadence(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
	if _, err := parseCadence("not-a-cadence"); err == nil {
		t.Error("parseCadence(\"not-a-cadence\"): want error, got nil")
	}
}

func TestParseDuration_Days(t *testing.T) {
	d, err := parseDuration("30d")
	if err != nil {
		t.Fatalf("parseDuration: %v", err)
	}
	if d != 30*24*time.Hour {
		t.Errorf("parseDuration(30d) = %v, want 720h", d)
	}
}
