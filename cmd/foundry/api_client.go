package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// apiClientTimeout bounds every HTTP call cmd/foundry's --api-addr code
// paths make against foundryd's HTTP API (internal/api, docs/PLAN.md
// Task 36).
const apiClientTimeout = 30 * time.Second

// sessionTokenPath returns where `foundry login` (cmd/foundry/login.go)
// wrote the session JWT: ~/.foundry/session.jwt.
func sessionTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".foundry", "session.jwt"), nil
}

// readSessionToken reads the session JWT `foundry login` wrote, for use
// as the Bearer token on API requests.
func readSessionToken() (string, error) {
	path, err := sessionTokenPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read session token (run `foundry login` first): %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// newAPIRequest builds a bearer-authenticated request against
// apiAddr+path, ready for (*http.Client).Do.
func newAPIRequest(method, apiAddr, path string, body []byte) (*http.Request, error) {
	token, err := readSessionToken()
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(apiAddr, "/")+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

// readAPIErrorMessage reads a non-2xx response body (internal/api's
// writeError produces {"error": "..."} JSON) for a useful error message.
func readAPIErrorMessage(resp *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(raw)
}
