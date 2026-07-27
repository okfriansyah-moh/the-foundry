package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// runPlanSubmitViaAPI implements the --api-addr path of
// `foundry plan submit` (docs/PLAN.md Task 36 dogfood): POSTs the plan
// bytes to foundryd's HTTP API (POST /v1/plans) instead of parsing
// locally, printing the same PlanSubmission JSON shape the local path
// prints.
func runPlanSubmitViaAPI(apiAddr string, planBody []byte) error {
	req, err := newAPIRequest(http.MethodPost, apiAddr, "/v1/plans", planBody)
	if err != nil {
		return fmt.Errorf("plan submit --api-addr: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: apiClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("plan submit --api-addr: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("plan submit --api-addr: %s: %s", resp.Status, readAPIErrorMessage(resp))
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("plan submit --api-addr: decode response: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
