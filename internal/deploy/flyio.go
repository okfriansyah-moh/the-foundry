package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultFlyAPIBaseURL is the Fly Machines API base. Overridable via
// FlyAdapter.BaseURL so a contract/cassette test can point the exact same code
// path at a recorded API surface (docs/PLAN.md Task 125).
const defaultFlyAPIBaseURL = "https://api.machines.dev"

// FlyAdapter is a real Fly.io deploy adapter: every method performs actual HTTP
// I/O against the Fly API (honouring its context), returns the real remote
// state, and Health performs an actual reachability check with a real failure
// path. It is invoked ONLY from a trusted kernel-side deployment activity —
// never inside the executor sandbox (Constitution C4/C13, docs/PLAN.md Task 125).
type FlyAdapter struct {
	// Token is the scoped Fly API token. Required.
	Token string
	// BaseURL overrides the Fly API endpoint (contract tests point this at an
	// httptest server). Empty uses defaultFlyAPIBaseURL.
	BaseURL string
	// HTTP is the client used for every call. Empty uses a client with a
	// bounded timeout; a caller-supplied client's timeout still applies
	// alongside the per-call context deadline.
	HTTP *http.Client
}

func (f FlyAdapter) appName(product string) string {
	return "foundry-" + strings.ToLower(strings.TrimSpace(product))
}

func (f FlyAdapter) baseURL() string {
	if strings.TrimSpace(f.BaseURL) != "" {
		return strings.TrimRight(f.BaseURL, "/")
	}
	return defaultFlyAPIBaseURL
}

func (f FlyAdapter) httpClient() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// deployRequest is the body sent to the Fly API deploy endpoint.
type deployRequest struct {
	App         string `json:"app"`
	Environment string `json:"environment"`
	Image       string `json:"image"`
}

// deployResponse is the real remote state the Fly API returns.
type deployResponse struct {
	App string `json:"app"`
	URL string `json:"url"`
	Ref string `json:"ref"`
}

// do performs one authenticated JSON request against the Fly API, honouring
// ctx, and decodes a deployResponse. A non-2xx status is a real error carrying
// the remote body, so a failed deploy is never mistaken for a success.
func (f FlyAdapter) do(ctx context.Context, method, path string, body any) (deployResponse, error) {
	if strings.TrimSpace(f.Token) == "" {
		return deployResponse{}, fmt.Errorf("deploy flyio: missing token")
	}
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return deployResponse{}, fmt.Errorf("deploy flyio: marshal request: %w", err)
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.baseURL()+path, buf)
	if err != nil {
		return deployResponse{}, fmt.Errorf("deploy flyio: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient().Do(req)
	if err != nil {
		return deployResponse{}, fmt.Errorf("deploy flyio: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deployResponse{}, fmt.Errorf("deploy flyio: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out deployResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &out); err != nil {
			return deployResponse{}, fmt.Errorf("deploy flyio: decode response: %w", err)
		}
	}
	return out, nil
}

// DeployPreview deploys artifact to product's preview app and returns the real
// remote state.
func (f FlyAdapter) DeployPreview(ctx context.Context, product, artifact string) (Record, error) {
	app := f.appName(product)
	resp, err := f.do(ctx, http.MethodPost, "/v1/apps/"+app+"/deploy", deployRequest{App: app, Environment: "preview", Image: artifact})
	if err != nil {
		return Record{}, err
	}
	return Record{Product: app, Environment: "preview", Ref: refOr(resp.Ref, artifact), VerificationMode: "synthetic-substitute", URL: resp.URL}, nil
}

// DeployProduction deploys artifact to product's production app and returns the
// real remote state.
func (f FlyAdapter) DeployProduction(ctx context.Context, product, artifact string) (Record, error) {
	app := f.appName(product)
	resp, err := f.do(ctx, http.MethodPost, "/v1/apps/"+app+"/deploy", deployRequest{App: app, Environment: "production", Image: artifact})
	if err != nil {
		return Record{}, err
	}
	return Record{Product: app, Environment: "production", Ref: refOr(resp.Ref, artifact), VerificationMode: "real-canary", URL: resp.URL}, nil
}

// Rollback re-deploys product's previous ref and returns the real remote state.
func (f FlyAdapter) Rollback(ctx context.Context, product, ref string) (Record, error) {
	app := f.appName(product)
	resp, err := f.do(ctx, http.MethodPost, "/v1/apps/"+app+"/rollback", deployRequest{App: app, Environment: "production", Image: ref})
	if err != nil {
		return Record{}, err
	}
	return Record{Product: app, Environment: "production", Ref: refOr(resp.Ref, ref), VerificationMode: "real-canary", URL: resp.URL}, nil
}

// Health performs an ACTUAL reachability check against url with a real failure
// path: a non-2xx response or a transport error is a health failure, so a
// production deploy that came up unhealthy is detectable (and rolled back by
// the kernel activity), never assumed healthy.
func (f FlyAdapter) Health(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("deploy flyio: health url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("deploy flyio: build health request: %w", err)
	}
	resp, err := f.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("deploy flyio: health check %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deploy flyio: health check %s: unhealthy status %d", url, resp.StatusCode)
	}
	return nil
}

func refOr(remote, fallback string) string {
	if strings.TrimSpace(remote) != "" {
		return remote
	}
	return fallback
}
