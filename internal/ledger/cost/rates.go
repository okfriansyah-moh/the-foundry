package cost

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// docs/PLAN.md Task 120 (COST-02): a per-model rate table so token counts become
// dollars deterministically when a provider reports no dollar figure. A model
// with no rate entry is a named refusal to *estimate* — recorded as unknown,
// never silently priced at a global default.

// ModelRate is the per-token price of one model, in USD per 1,000 tokens.
type ModelRate struct {
	Model               string  `yaml:"model"`
	InputPer1KUSD       float64 `yaml:"input_per_1k_usd"`
	OutputPer1KUSD      float64 `yaml:"output_per_1k_usd"`
	CachedInputPer1KUSD float64 `yaml:"cached_input_per_1k_usd"`
}

// RateTable is the loaded set of per-model rates.
type RateTable struct {
	Models  []ModelRate `yaml:"models"`
	byModel map[string]ModelRate
}

// LoadRateTable reads and indexes the per-model rate table.
func LoadRateTable(path string) (RateTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RateTable{}, fmt.Errorf("cost: read rate table %s: %w", path, err)
	}
	var rt RateTable
	if err := yaml.Unmarshal(raw, &rt); err != nil {
		return RateTable{}, fmt.Errorf("cost: parse rate table %s: %w", path, err)
	}
	rt.index()
	return rt, nil
}

// NewRateTable builds an indexed RateTable from rates (for tests/wiring).
func NewRateTable(rates ...ModelRate) RateTable {
	rt := RateTable{Models: rates}
	rt.index()
	return rt
}

func (rt *RateTable) index() {
	rt.byModel = make(map[string]ModelRate, len(rt.Models))
	for _, r := range rt.Models {
		rt.byModel[r.Model] = r
	}
}

// PriceUnknownError is returned when a model has no rate entry — the deliberate
// refusal to fabricate an estimate (Task 120).
type PriceUnknownError struct{ Model string }

func (e PriceUnknownError) Error() string {
	return fmt.Sprintf("cost: no rate entry for model %q — refusing to estimate (recorded as unknown)", e.Model)
}

// PriceUsage prices token usage in dollars. It returns:
//   - the provider's own reported dollar figure when present (authoritative),
//   - otherwise a deterministic figure from the rate table,
//   - otherwise a PriceUnknownError (no rate for the model) so the caller
//     records "unknown" rather than a fabricated default.
func (rt RateTable) PriceUsage(model string, inputTokens, outputTokens, cachedTokens int, providerReportedUSD float64) (float64, error) {
	if providerReportedUSD > 0 {
		return providerReportedUSD, nil
	}
	if inputTokens == 0 && outputTokens == 0 {
		// No token signal and no dollar figure: unknown, not zero.
		return 0, PriceUnknownError{Model: model}
	}
	rate, ok := rt.byModel[model]
	if !ok {
		return 0, PriceUnknownError{Model: model}
	}
	usd := float64(inputTokens)/1000*rate.InputPer1KUSD +
		float64(outputTokens)/1000*rate.OutputPer1KUSD +
		float64(cachedTokens)/1000*rate.CachedInputPer1KUSD
	return usd, nil
}
