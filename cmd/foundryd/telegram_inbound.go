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
