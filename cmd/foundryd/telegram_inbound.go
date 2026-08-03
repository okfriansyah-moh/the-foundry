package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

// docs/PLAN.md Task 112 (INT-04): wire a real inbound Telegram transport into
// foundryd — a CommandRouter over the production store/nonce/chat registry and
// a Temporal-backed WorkflowController, fed by a durable-offset getUpdates
// receiver. Env-gated: with no FOUNDRY_TELEGRAM_BOT_TOKEN it is inert, matching
// the outbound engine's own credential gating.

// temporalWorkflowController implements notify.WorkflowController against a live
// Temporal client. It performs only the mission operator signals the command
// vocabulary already exposes (Constitution C4: it signals, it never decides).
type temporalWorkflowController struct {
	client    client.Client
	namespace string
}

// Status returns a coarse workflow status via DescribeWorkflowExecution.
func (c temporalWorkflowController) Status(ctx context.Context, workflow string) (string, error) {
	desc, err := c.client.DescribeWorkflowExecution(ctx, workflow, "")
	if err != nil {
		return "", fmt.Errorf("describe %s: %w", workflow, err)
	}
	if desc.GetWorkflowExecutionInfo() == nil {
		return "unknown", nil
	}
	return desc.GetWorkflowExecutionInfo().GetStatus().String(), nil
}

// Pause signals an operator-initiated mission pause.
func (c temporalWorkflowController) Pause(ctx context.Context, workflow string) error {
	return c.client.SignalWorkflow(ctx, workflow, "", mission.SignalManualPause, nil)
}

// Resume signals a mission resume.
func (c temporalWorkflowController) Resume(ctx context.Context, workflow string) error {
	return c.client.SignalWorkflow(ctx, workflow, "", mission.SignalResumeMission, nil)
}

// startTelegramInbound constructs and starts the inbound receiver if a bot
// token is configured. It returns immediately after launching the poller.
func startTelegramInbound(ctx context.Context, db *sql.DB, tc client.Client, namespace string) {
	token := os.Getenv("FOUNDRY_TELEGRAM_BOT_TOKEN")
	if token == "" {
		return // inbound transport inert without production bot credentials
	}
	store := notify.NewPostgresStore(db)
	chats := notify.NewChatRegistry()
	if opsChat := os.Getenv("FOUNDRY_OPS_CHAT_ID"); opsChat != "" {
		principal := envOr("FOUNDRY_OPS_PRINCIPAL", "ops")
		chats.Register(opsChat, principal)
	}
	router := &notify.CommandRouter{
		Chats:      chats,
		Nonces:     notify.NewNonceRegistry(),
		Controller: temporalWorkflowController{client: tc, namespace: namespace},
		FreezeEvolution: func(freezeCtx context.Context, reason evolve.FreezeCondition) error {
			return evolve.NewFreezeStore(db).Freeze(freezeCtx, evolve.FreezeScopeGlobal, reason)
		},
	}
	sender := &notify.HTTPSender{Token: token}
	botID := hashBotID(token)
	receiver, err := notify.NewReceiver(notify.ReceiverConfig{
		BotID: botID, Token: token, PollTimeout: 30,
	}, store, router, sender)
	if err != nil {
		log.Printf("foundryd: telegram inbound disabled: %v", err)
		return
	}
	go func() {
		if err := receiver.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("foundryd: telegram receiver: %v", err)
		}
	}()
}

// hashBotID derives a stable, non-secret bot id for the durable offset key from
// the bot token, so the token itself never lands in the telegram_offsets table.
func hashBotID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "bot-" + hex.EncodeToString(sum[:8])
}

// checkStrongAuthIdP names the missing OIDC variable when strong auth is enabled
// but no issuer is configured (docs/PLAN.md Task 114 / INT-06, step 3a). The
// identity provider is configuration, not code: FOUNDRY_OIDC_ISSUER and
// FOUNDRY_OIDC_CLIENT_ID default (in .env.example / deploy) to a hosted
// Zitadel-class free tier; the fake IdP remains the CI path. Strong auth is
// treated as enabled whenever a WebAuthn RP origin is explicitly configured.
func checkStrongAuthIdP() error {
	strongAuthEnabled := os.Getenv("FOUNDRY_WEBAUTHN_ORIGIN") != "" || os.Getenv("FOUNDRY_OIDC_ISSUER") != ""
	if !strongAuthEnabled {
		return nil
	}
	if os.Getenv("FOUNDRY_OIDC_ISSUER") == "" {
		return fmt.Errorf(
			"strong auth is enabled but FOUNDRY_OIDC_ISSUER is unset: set it (and FOUNDRY_OIDC_CLIENT_ID) " +
				"to your IdP — see .env.example for the hosted Zitadel-class default, or point it at test/fakes/oidc in CI")
	}
	if os.Getenv("FOUNDRY_OIDC_CLIENT_ID") == "" {
		return fmt.Errorf("FOUNDRY_OIDC_ISSUER is set but FOUNDRY_OIDC_CLIENT_ID is unset — both are required for `foundry login`")
	}
	return nil
}
