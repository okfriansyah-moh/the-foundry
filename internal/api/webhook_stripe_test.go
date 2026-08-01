package api

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
	stripe "github.com/stripe/stripe-go/v79"
	stripewebhook "github.com/stripe/stripe-go/v79/webhook"

	"github.com/okfriansyah-moh/the-foundry/internal/billing"
)

type recordingWebhook struct {
	called bool
}

func (h *recordingWebhook) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusNoContent)
}

func TestStripeWebhookRouteDelegatesWithoutBearerSession(t *testing.T) {
	f := newTestFixture(t)
	hook := &recordingWebhook{}
	f.server.deps.StripeWebhook = hook
	f.server.registerPublic(http.MethodPost, "/v1/stripe/webhook", f.server.handleStripeWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/stripe/webhook", strings.NewReader(`{}`))
	req.Header.Set("Stripe-Signature", "t=1,v1=test")
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !hook.called {
		t.Fatal("stripe webhook handler was not called")
	}
}

func TestStripeWebhookRouteVerifiesSignatureAndPersists(t *testing.T) {
	dsn := os.Getenv("API_BILLING_TEST_PG_DSN")
	if dsn == "" {
		dsn = os.Getenv("PG_DSN")
	}
	if dsn == "" {
		t.Skip("API_BILLING_TEST_PG_DSN/PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("PG_DSN set but unreachable: %v", err)
	}
	prepareAPIBillingTables(t, db)
	defer func() { _ = db.Close() }()

	secret := "whsec_api_route"
	handler, err := billing.NewWebhookHandler(secret, billing.NewEventStore(db))
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}
	f := newTestFixture(t)
	f.server.deps.StripeWebhook = handler
	f.server.registerPublic(http.MethodPost, "/v1/stripe/webhook", f.server.handleStripeWebhook)

	eventID := fmt.Sprintf("evt_api_%d", time.Now().UnixNano())
	payload := []byte(fmt.Sprintf(`{"id":%q,"object":"event","api_version":%q,"created":%d,"livemode":false,"type":"checkout.session.completed","data":{"object":{"id":"cs_test_api","object":"checkout.session"}}}`, eventID, stripe.APIVersion, time.Now().Unix()))
	bad := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: payload, Secret: "whsec_wrong", Timestamp: time.Now()})
	badReq := httptest.NewRequest(http.MethodPost, "/v1/stripe/webhook", strings.NewReader(string(payload)))
	badReq.Header.Set("Stripe-Signature", bad.Header)
	badRec := httptest.NewRecorder()
	f.server.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("forged status=%d, want 401", badRec.Code)
	}

	good := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: payload, Secret: secret, Timestamp: time.Now()})
	goodReq := httptest.NewRequest(http.MethodPost, "/v1/stripe/webhook", strings.NewReader(string(payload)))
	goodReq.Header.Set("Stripe-Signature", good.Header)
	goodRec := httptest.NewRecorder()
	f.server.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusNoContent {
		t.Fatalf("valid status=%d, want 204", goodRec.Code)
	}

	if _, err := billing.NewEventStore(db).Get(ctx, eventID); err != nil {
		t.Fatalf("load persisted event: %v", err)
	}
}

func prepareAPIBillingTables(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS stripe_events (id TEXT PRIMARY KEY, received_at TIMESTAMPTZ NOT NULL DEFAULT now(), payload JSONB NOT NULL)`,
		`ALTER TABLE stripe_events ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'unknown', ADD COLUMN IF NOT EXISTS livemode BOOLEAN NOT NULL DEFAULT false, ADD COLUMN IF NOT EXISTS stripe_created_at TIMESTAMPTZ, ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("prepare Stripe event table: %v", err)
		}
	}
}
