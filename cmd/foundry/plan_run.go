package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

// runPlanRun implements `foundry plan run` (docs/PLAN.md Task 105 / RTC-01): a
// thin client of POST /v1/plans/{id}/deliver, the single production edge from
// an ApprovedPlan to a running DeliverPlan execution. CLI/API parity (Task
// 36): the kernel — not this CLI — resolves the lane, executor allowlist and
// workflow ID, so this command passes only a plan id and an optional lane.
func runPlanRun(args []string) error {
	fs := flag.NewFlagSet("plan run", flag.ContinueOnError)
	apiAddr := fs.String("api-addr", apiAddrDefault(), "foundryd HTTP API address")
	planID := fs.String("plan-id", "", "approved plan id to deliver (required)")
	lane := fs.String("lane", "", "optional queue-priority lane")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *planID == "" {
		return fmt.Errorf("plan run: --plan-id is required")
	}

	path := "/v1/plans/" + url.PathEscape(*planID) + "/deliver"
	if *lane != "" {
		path += "?lane=" + url.QueryEscape(*lane)
	}
	req, err := newAPIRequest(http.MethodPost, *apiAddr, path, nil)
	if err != nil {
		return fmt.Errorf("plan run: %w", err)
	}

	client := &http.Client{Timeout: apiClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("plan run: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("plan run: %s: %s", resp.Status, readAPIErrorMessage(resp))
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("plan run: decode response: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func apiAddrDefault() string {
	if v := os.Getenv("FOUNDRY_API_ADDR"); v != "" {
		return v
	}
	return "http://localhost:8081"
}
