package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

// apiStatusResponse mirrors internal/api's statusResponse JSON shape
// (internal/api/status.go) — duplicated rather than imported, since
// internal/api's type is unexported and this is a three-field decode, not
// shared business logic.
type apiStatusResponse struct {
	WorkflowID     string  `json:"workflow_id"`
	Status         string  `json:"status"`
	Phase          string  `json:"phase"`
	Consistency    string  `json:"consistency"`
	LastSeq        int64   `json:"last_seq"`
	LagSeconds     float64 `json:"lag_seconds"`
	TemporalStatus string  `json:"temporal_status"`
}

// runStatusViaAPI implements the --api-addr path of `foundry status`
// (docs/PLAN.md Task 36 dogfood): calls GET /v1/workflows/{id}/status on
// a running foundryd instead of querying Postgres/Temporal directly,
// authenticating with the session JWT `foundry login` wrote. Output
// mirrors the direct-DB path's formatProjected/formatFresh exactly, so a
// caller cannot tell which path produced it from the text alone.
func runStatusViaAPI(parsed statusArgs) error {
	path := "/v1/workflows/" + url.PathEscape(parsed.workflowID) + "/status"
	if parsed.fresh {
		path += "?consistency=fresh"
	}

	req, err := newAPIRequest(http.MethodGet, parsed.apiAddr, path, nil)
	if err != nil {
		return fmt.Errorf("status --api-addr: %w", err)
	}

	client := &http.Client{Timeout: apiClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("status --api-addr: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status --api-addr: %s: %s", resp.Status, readAPIErrorMessage(resp))
	}

	var body apiStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("status --api-addr: decode response: %w", err)
	}

	if body.Consistency == "fresh" {
		fmt.Fprintf(os.Stdout,
			"workflow_id: %s\nstatus: %s\nphase: %s\ntemporal_status: %s\nconsistency: fresh\n",
			body.WorkflowID, body.Status, body.Phase, body.TemporalStatus,
		)
		return nil
	}
	fmt.Fprintf(os.Stdout,
		"workflow_id: %s\nstatus: %s\nphase: %s\nlast_seq: %d\nconsistency: projected (lag: %.0fs)\n",
		body.WorkflowID, body.Status, body.Phase, body.LastSeq, body.LagSeconds,
	)
	return nil
}
