package authn_test

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	fakeoidc "github.com/okfriansyah-moh/the-foundry/test/fakes/oidc"
)

func TestDeviceLogin_EndToEnd(t *testing.T) {
	idp, err := fakeoidc.NewServer()
	if err != nil {
		t.Fatalf("start fake idp: %v", err)
	}
	defer idp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt, err := authn.StartDeviceLogin(ctx, authn.LoginConfig{
		IssuerURL: idp.URL,
		ClientID:  idp.ClientID,
	})
	if err != nil {
		t.Fatalf("StartDeviceLogin: %v", err)
	}
	if prompt.UserCode == "" || prompt.VerificationURI == "" {
		t.Fatalf("prompt missing verification fields: %+v", prompt)
	}

	sessionKey, err := authn.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}

	result, err := authn.FinishDeviceLogin(ctx, prompt, sessionKey)
	if err != nil {
		t.Fatalf("FinishDeviceLogin: %v", err)
	}
	if result.Principal != fakeoidc.Subject {
		t.Fatalf("principal = %q, want %q", result.Principal, fakeoidc.Subject)
	}

	principal, err := authn.VerifySession(&sessionKey.PublicKey, result.SessionToken)
	if err != nil {
		t.Fatalf("VerifySession(issued token): %v", err)
	}
	if principal != fakeoidc.Subject {
		t.Fatalf("session principal = %q, want %q", principal, fakeoidc.Subject)
	}
}

func TestStartDeviceLogin_RejectsBadIssuer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := authn.StartDeviceLogin(ctx, authn.LoginConfig{
		IssuerURL: "http://127.0.0.1:1", // nothing listens here
		ClientID:  "whatever",
	})
	if err == nil {
		t.Fatal("expected discovery against an unreachable issuer to fail")
	}
}
