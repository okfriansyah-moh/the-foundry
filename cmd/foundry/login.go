package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
)

// runLogin implements `foundry login` (docs/PLAN.md Task 25 Step 1):
// device-code OIDC login against --issuer-url (Blocker B5: managed IdP,
// Zitadel-class OIDC in production; test/fakes/oidc in tests), followed by
// a Foundry session JWT signed under the local session key
// (~/.foundry/keys/session.key — shared with whatever process runs
// ApproveHandler) and written to ~/.foundry/session.jwt (0600).
func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	issuerURL := fs.String("issuer-url", os.Getenv("FOUNDRY_OIDC_ISSUER"), "OIDC issuer URL (Blocker B5: managed IdP)")
	clientID := fs.String("client-id", os.Getenv("FOUNDRY_OIDC_CLIENT_ID"), "OIDC client id")
	scopes := fs.String("scopes", os.Getenv("FOUNDRY_OIDC_SCOPES"), "space-separated OIDC scopes (default 'openid'; 'profile email' bind a human approver identity)")
	keyDir := fs.String("key-dir", "", "session key directory (default ~/.foundry/keys)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *issuerURL == "" {
		return fmt.Errorf("login: --issuer-url (or FOUNDRY_OIDC_ISSUER) is required")
	}
	if *clientID == "" {
		return fmt.Errorf("login: --client-id (or FOUNDRY_OIDC_CLIENT_ID) is required")
	}

	dir := *keyDir
	if dir == "" {
		d, err := authn.DefaultSessionKeyDir()
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		dir = d
	}
	sessionKey, err := authn.LoadOrGenerateSessionKey(dir)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	prompt, err := authn.StartDeviceLogin(ctx, authn.LoginConfig{IssuerURL: *issuerURL, ClientID: *clientID, Scopes: parseScopes(*scopes)})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	fmt.Fprintf(os.Stderr, "To finish signing in, visit %s and enter code: %s\n", prompt.VerificationURI, prompt.UserCode)

	result, err := authn.FinishDeviceLogin(ctx, prompt, sessionKey)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("login: resolve home dir: %w", err)
	}
	sessionPath := filepath.Join(home, ".foundry", "session.jwt")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
		return fmt.Errorf("login: create session dir: %w", err)
	}
	if err := os.WriteFile(sessionPath, result.SessionToken, 0o600); err != nil {
		return fmt.Errorf("login: write session token: %w", err)
	}

	fmt.Printf("logged in as %s (session written to %s)\n", result.Principal, sessionPath)
	return nil
}

// parseScopes splits a space-separated scope set (FOUNDRY_OIDC_SCOPES); empty
// yields nil, which internal/authn defaults to {openid}.
func parseScopes(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	return fields
}
