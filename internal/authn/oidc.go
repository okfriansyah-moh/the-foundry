package authn

import (
	"context"
	"crypto/ecdsa"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// LoginConfig configures a device-code OIDC login (docs/PLAN.md Task 25 /
// Blocker B5: managed IdP, Zitadel-class OIDC by default). IssuerURL must
// serve OIDC discovery (/.well-known/openid-configuration); test/fakes/oidc
// provides one for tests that have no live IdP to talk to.
type LoginConfig struct {
	IssuerURL string
	ClientID  string
	// Scopes defaults to {oidc.ScopeOpenID} when empty.
	Scopes []string
}

// DeviceCodePrompt is what a human approves out of band (typically in a
// browser) before FinishDeviceLogin can succeed. It carries the
// provider/config state FinishDeviceLogin needs, unexported so callers
// cannot construct or mutate a prompt outside StartDeviceLogin.
type DeviceCodePrompt struct {
	// VerificationURI and UserCode are what a CLI displays to the human
	// (e.g. "foundry login" prints: "go to <VerificationURI> and enter
	// <UserCode>").
	VerificationURI         string
	VerificationURIComplete string
	UserCode                string

	resp     *oauth2.DeviceAuthResponse
	conf     *oauth2.Config
	provider *oidc.Provider
}

// StartDeviceLogin begins the RFC 8628 device authorization grant against
// cfg.IssuerURL: it discovers the provider, requests a device code, and
// returns the prompt for a human to approve.
func StartDeviceLogin(ctx context.Context, cfg LoginConfig) (*DeviceCodePrompt, error) {
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("authn: oidc login: issuer url is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("authn: oidc login: client id is required")
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID}
	}

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("authn: oidc discovery against %s: %w", cfg.IssuerURL, err)
	}
	conf := &oauth2.Config{
		ClientID: cfg.ClientID,
		Endpoint: provider.Endpoint(),
		Scopes:   scopes,
	}
	resp, err := conf.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("authn: device authorization request: %w", err)
	}
	return &DeviceCodePrompt{
		VerificationURI:         resp.VerificationURI,
		VerificationURIComplete: resp.VerificationURIComplete,
		UserCode:                resp.UserCode,
		resp:                    resp,
		conf:                    conf,
		provider:                provider,
	}, nil
}

// LoginResult is the outcome of a completed device-code login: the
// verified OIDC principal (the ID token's "sub" claim) and a freshly
// issued Foundry session JWT bound to it.
type LoginResult struct {
	Principal    string
	SessionToken []byte
}

// FinishDeviceLogin polls the token endpoint (per the device flow's
// server-configured interval) until the human approves prompt's user
// code, verifies the returned ID token against the provider's published
// keys — via oidc.IDTokenVerifier, never by hand-parsing the JWT — and
// issues a Foundry session JWT for the verified principal, signed under
// sessionKey.
func FinishDeviceLogin(ctx context.Context, prompt *DeviceCodePrompt, sessionKey *ecdsa.PrivateKey) (*LoginResult, error) {
	if prompt == nil {
		return nil, fmt.Errorf("authn: finish device login: prompt is required")
	}
	tok, err := prompt.conf.DeviceAccessToken(ctx, prompt.resp)
	if err != nil {
		return nil, fmt.Errorf("authn: device access token: %w", err)
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("authn: token response has no id_token")
	}
	verifier := prompt.provider.Verifier(&oidc.Config{ClientID: prompt.conf.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("authn: verify id_token: %w", err)
	}
	var claims struct {
		Subject string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("authn: decode id_token claims: %w", err)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("authn: id_token has empty sub")
	}
	session, err := IssueSession(sessionKey, claims.Subject)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Principal: claims.Subject, SessionToken: session}, nil
}
