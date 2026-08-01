package billing

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestStripeLiveSignatureForgeryNegative(t *testing.T) {
	payload, badHeader := signedStripePayload("evt_live_forged", "whsec_wrong")
	handler, err := NewWebhookHandler("whsec_right", &EventStore{})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", badHeader)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestLiveStripeCheckoutWebhookDurableEventAndReconciledRevenue(t *testing.T) {
	if os.Getenv("RUN_STRIPE") != "1" {
		t.Skip("RUN_STRIPE=1 not set")
	}
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set")
	}
	dsn := billingTestDSN()
	if dsn == "" {
		t.Skip("BILLING_TEST_PG_DSN/PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	prepareBillingTables(t, db)

	client, err := NewStripeClient(ClientConfig{SecretKey: key})
	if err != nil {
		t.Fatalf("NewStripeClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := client.CreateCheckoutSession(ctx, CheckoutSessionRequest{
		CustomerEmail: fmt.Sprintf("stripe-live-test-%d@example.com", time.Now().UnixNano()),
		AmountCents:   1234,
		Currency:      "usd",
		ProductName:   "Foundry live billing path test",
		SuccessURL:    "https://example.com/success",
		CancelURL:     "https://example.com/cancel",
	})
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	if sess.ID == "" || sess.AmountCents != 1234 {
		t.Fatalf("checkout session=%+v, want real id and amount 1234", sess)
	}

	secret := "whsec_live_path_test"
	eventID := "evt_live_path_" + sess.ID
	payload, header := signedStripePayload(eventID, secret)
	handler, err := NewWebhookHandler(secret, NewEventStore(db))
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", header)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("webhook status=%d, want 204", rec.Code)
	}

	reconciler := NewReconciler(client, NewRevenueStore(db))
	if err := reconciler.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := NewRevenueStore(db).Latest(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("latest revenue row: %v", err)
	}
}
