// Command approval_stepup_e2e_client is test-only tooling for
// test/approval_stepup_e2e.sh (docs/PLAN.md Task 25). It has three modes,
// each a small standalone piece the shell script cannot do on its own:
//
//   - idp: runs test/fakes/oidc's fake OIDC IdP on a real port and writes
//     its URL/client_id to -info-file, so `foundry login` (a real
//     subprocess) has something real to talk to.
//   - server: runs a real net/http server hosting this task's
//     ApproveHandler and WebAuthnHTTP, backed by in-memory fakes (no live
//     Postgres needed), preloaded with one High-tier plan ("plan-h").
//   - approve: acts as the WebAuthn-capable browser client would — it
//     registers a credential, begins a login ceremony, and completes the
//     approval — using go-webauthn/virtualwebauthn for the ceremony
//     itself (bash/curl cannot perform WebAuthn's asymmetric signing;
//     this task's own Boundary forbids hand-rolling it instead).
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	oidcfake "github.com/okfriansyah-moh/the-foundry/test/fakes/oidc"
)

// rpID/rpOrigin are shared between the "server" and "approve" modes (two
// separate process invocations) so their WebAuthn ceremonies agree on the
// same relying party, mirroring internal/authn's own test constants.
const (
	rpID     = "example.com"
	rpOrigin = "https://example.com"
)

func main() {
	mode := flag.String("mode", "", "idp|server|approve")
	infoFile := flag.String("info-file", "", "file to write connection info to")
	sessionKeyPath := flag.String("session-key", "", "PEM EC private key file (server mode)")
	serverURL := flag.String("server-url", "", "approve server base URL (approve mode)")
	sessionToken := flag.String("session-token", "", "bearer session token (approve mode)")
	planID := flag.String("plan-id", "", "plan id to approve (approve mode)")
	flag.Parse()

	var err error
	switch *mode {
	case "idp":
		err = runIDP(*infoFile)
	case "server":
		err = runServer(*sessionKeyPath, *infoFile)
	case "approve":
		err = runApprove(*serverURL, *sessionToken, *planID)
	default:
		err = fmt.Errorf("unknown -mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runIDP(infoFile string) error {
	srv, err := oidcfake.NewServer()
	if err != nil {
		return err
	}
	defer srv.Close()
	if err := os.WriteFile(infoFile, []byte(srv.URL+"\n"+srv.ClientID+"\n"), 0o600); err != nil {
		return err
	}
	waitForSignal()
	return nil
}

func runServer(sessionKeyPath, infoFile string) error {
	sessionPub, err := loadSessionPublicKey(sessionKeyPath)
	if err != nil {
		return err
	}

	raw := provenance.NewMemRawStore()
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		return err
	}
	store := provenance.NewStore(raw, kp.Public)

	planH, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:     "plan-h",
		PlanDigest: "sha256:plan-h",
		RiskTier:   admission.TierH,
		ApprovedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		return err
	}
	if err := provenance.Sign(kp.Private, planH); err != nil {
		return err
	}
	if err := store.Insert(context.Background(), planH); err != nil {
		return err
	}

	waSvc, err := authn.NewService(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "Approval Stepup E2E",
		RPOrigins:     []string{rpOrigin},
	}, authn.NewMemUserStore())
	if err != nil {
		return err
	}

	approveHandler := &authn.ApproveHandler{
		SessionPub: sessionPub,
		WebAuthn:   waSvc,
		Store:      store,
		SigningKey: kp.Private,
		ResolveContext: func(_ context.Context, planID string) (authn.PlanContext, error) {
			if planID != "plan-h" {
				return authn.PlanContext{}, fmt.Errorf("unknown plan %s", planID)
			}
			return authn.PlanContext{Tier: admission.TierH, Profile: profile.Personal}, nil
		},
	}
	webauthnHTTP := &authn.WebAuthnHTTP{SessionPub: sessionPub, Service: waSvc}

	mux := http.NewServeMux()
	mux.Handle("POST /v1/plans/{id}/approve", approveHandler)
	mux.HandleFunc("POST /v1/webauthn/register/begin", webauthnHTTP.BeginRegistration)
	mux.HandleFunc("POST /v1/webauthn/register/finish", webauthnHTTP.FinishRegistration)
	mux.HandleFunc("POST /v1/webauthn/login/begin", webauthnHTTP.BeginLogin)

	ln, err := newLocalListener()
	if err != nil {
		return err
	}
	if err := os.WriteFile(infoFile, []byte("http://"+ln.Addr().String()), 0o600); err != nil {
		return err
	}

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	waitForSignal()
	return nil
}

func runApprove(serverURL, sessionToken, planID string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	authHeader := "Bearer " + sessionToken

	rp := virtualwebauthn.RelyingParty{Name: "Approval Stepup E2E", ID: rpID, Origin: rpOrigin}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	// Register.
	regBegin, err := postJSON(client, serverURL+"/v1/webauthn/register/begin", authHeader, nil)
	if err != nil {
		return fmt.Errorf("register/begin: %w", err)
	}
	var regBeginBody struct {
		SessionID string          `json:"session_id"`
		Options   json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(regBegin, &regBeginBody); err != nil {
		return fmt.Errorf("decode register/begin: %w", err)
	}
	attOpts, err := virtualwebauthn.ParseAttestationOptions(string(regBeginBody.Options))
	if err != nil {
		return fmt.Errorf("parse attestation options: %w", err)
	}
	attResp := virtualwebauthn.CreateAttestationResponse(rp, authr, cred, *attOpts)
	if _, err := postJSON(client, serverURL+"/v1/webauthn/register/finish?session_id="+regBeginBody.SessionID, authHeader, []byte(attResp)); err != nil {
		return fmt.Errorf("register/finish: %w", err)
	}
	authr.AddCredential(cred)

	// Login (step-up) + approve.
	loginBegin, err := postJSON(client, serverURL+"/v1/webauthn/login/begin", authHeader, nil)
	if err != nil {
		return fmt.Errorf("login/begin: %w", err)
	}
	var loginBeginBody struct {
		SessionID string          `json:"session_id"`
		Options   json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(loginBegin, &loginBeginBody); err != nil {
		return fmt.Errorf("decode login/begin: %w", err)
	}
	assertionOpts, err := virtualwebauthn.ParseAssertionOptions(string(loginBeginBody.Options))
	if err != nil {
		return fmt.Errorf("parse assertion options: %w", err)
	}
	assertionResp := virtualwebauthn.CreateAssertionResponse(rp, authr, cred, *assertionOpts)

	body := fmt.Sprintf(`{"webauthn_session_id":%q,"webauthn_assertion":%s}`, loginBeginBody.SessionID, assertionResp)
	req, err := http.NewRequest(http.MethodPost, serverURL+"/v1/plans/"+planID+"/approve", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("approve: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("approve: status = %d, want 200", resp.StatusCode)
	}
	fmt.Println("approve with WebAuthn step-up: 200 OK")
	return nil
}

func postJSON(client *http.Client, url, authHeader string, body []byte) ([]byte, error) {
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if readErr != nil {
			break
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: status %d: %s", url, resp.StatusCode, string(buf))
	}
	return buf, nil
}

func newLocalListener() (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	return ln, nil
}

func loadSessionPublicKey(path string) (*ecdsa.PublicKey, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session key %s: %w", path, err)
	}
	block, _ := pem.Decode(buf)
	if block == nil {
		return nil, fmt.Errorf("%s is not valid PEM", path)
	}
	priv, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse session key %s: %w", path, err)
	}
	return &priv.PublicKey, nil
}

func waitForSignal() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	<-sigs
}
