package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	stripe "github.com/stripe/stripe-go/v79"
	stripewebhook "github.com/stripe/stripe-go/v79/webhook"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
)

func billingTestDSN() string {
	if v := os.Getenv("BILLING_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

func openBillingTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := billingTestDSN()
	if dsn == "" {
		t.Skip("BILLING_TEST_PG_DSN/PG_DSN not set — skipping real Postgres billing test")
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
	prepareBillingTables(t, db)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func prepareBillingTables(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS stripe_events (
			id TEXT PRIMARY KEY,
			received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			payload JSONB NOT NULL
		)`,
		`ALTER TABLE stripe_events
			ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'unknown',
			ADD COLUMN IF NOT EXISTS livemode BOOLEAN NOT NULL DEFAULT false,
			ADD COLUMN IF NOT EXISTS stripe_created_at TIMESTAMPTZ,
			ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ`,
		`CREATE TABLE IF NOT EXISTS revenue_reconciliation (
			id TEXT PRIMARY KEY,
			at TIMESTAMPTZ NOT NULL,
			subscriptions_usd NUMERIC(12,4) NOT NULL,
			refunds_usd NUMERIC(12,4) NOT NULL,
			cancellations_usd NUMERIC(12,4) NOT NULL,
			discounts_usd NUMERIC(12,4) NOT NULL,
			net_mrr_usd NUMERIC(12,4) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE revenue_reconciliation
			ADD COLUMN IF NOT EXISTS stripe_balance_transaction_id TEXT,
			ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'stripe_test_mode'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS revenue_reconciliation_stripe_balance_transaction_id_key
			ON revenue_reconciliation (stripe_balance_transaction_id)
			WHERE stripe_balance_transaction_id IS NOT NULL`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("prepare billing table: %v\n%s", err, stmt)
		}
	}
}

func signedStripePayload(eventID, secret string) ([]byte, string) {
	payload := []byte(fmt.Sprintf(`{"id":%q,"object":"event","api_version":%q,"created":%d,"livemode":false,"type":"checkout.session.completed","data":{"object":{"id":"cs_test_123","object":"checkout.session"}}}`, eventID, stripe.APIVersion, time.Now().Unix()))
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	})
	return payload, signed.Header
}

func TestWebhookRejectsForgedSignature(t *testing.T) {
	payload, badHeader := signedStripePayload("evt_forged", "whsec_wrong")
	h, err := NewWebhookHandler("whsec_right", &EventStore{})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", badHeader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestWebhookReplayIdempotentDurable(t *testing.T) {
	db := openBillingTestDB(t)
	secret := "whsec_replay_test"
	eventID := fmt.Sprintf("evt_replay_%d", time.Now().UnixNano())
	payload, header := signedStripePayload(eventID, secret)

	h1, err := NewWebhookHandler(secret, NewEventStore(db))
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}
	req1 := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	req1.Header.Set("Stripe-Signature", header)
	rec1 := httptest.NewRecorder()
	h1.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first status=%d, want 204", rec1.Code)
	}
	_ = db.Close()

	db2, err := sql.Open("pgx", billingTestDSN())
	if err != nil {
		t.Fatalf("reopen sql.Open: %v", err)
	}
	defer func() { _ = db2.Close() }()
	h2, err := NewWebhookHandler(secret, NewEventStore(db2))
	if err != nil {
		t.Fatalf("NewWebhookHandler reopened: %v", err)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	req2.Header.Set("Stripe-Signature", header)
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("replay status=%d, want 204", rec2.Code)
	}

	var count int
	if err := db2.QueryRow(`SELECT count(*) FROM stripe_events WHERE id = $1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count event: %v", err)
	}
	if count != 1 {
		t.Fatalf("durable event count=%d, want 1", count)
	}
}

func TestReconcile_TestClockScenarioMatchesCent(t *testing.T) {
	row := Reconcile(ReconciliationInput{
		SubscriptionsUSD: 300.00,
		RefundsUSD:       100.00,
		CancellationsUSD: 0,
		DiscountsUSD:     0,
		At:               time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	want := 200.00
	if math.Abs(row.NetMRRUSD-want) > 0.0001 {
		t.Fatalf("net mrr=%.4f, want %.4f", row.NetMRRUSD, want)
	}
}

type fakeRevenueProvider struct {
	txs  []BalanceTransaction
	subs []Subscription
}

func (f fakeRevenueProvider) ListBalanceTransactions(context.Context, time.Time, time.Time) ([]BalanceTransaction, error) {
	return f.txs, nil
}

func (f fakeRevenueProvider) ListSubscriptions(context.Context, string) ([]Subscription, error) {
	return f.subs, nil
}

func TestReconcilerWritesRevenueAndMissionObservesIt(t *testing.T) {
	db := openBillingTestDB(t)
	store := NewRevenueStore(db)
	now := time.Now().UTC()
	provider := fakeRevenueProvider{
		subs: []Subscription{{
			ID:     "sub_test",
			Status: "active",
			Items:  []SubscriptionItem{{Currency: "usd", UnitAmount: 30000, Quantity: 1, Interval: "month", IntervalCount: 1}},
		}},
		txs: []BalanceTransaction{{ID: "txn_refund", Type: "refund", Currency: "usd", Net: -10000, CreatedAt: now}},
	}

	reconciler := NewReconciler(provider, store, WithReconcilerNow(func() time.Time { return now }))
	if err := reconciler.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sample, err := (MissionNetMRRSource{Store: store}).Observe(context.Background(), "mission-1", now.Add(time.Second))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !sample.Available {
		t.Fatal("mission sample unavailable, want latest reconciled revenue")
	}
	if got, want := sample.NetMRRUSD(), 200.0; math.Abs(got-want) > 0.0001 {
		t.Fatalf("NetMRRUSD()=%.4f, want %.4f", got, want)
	}
}

func TestUnavailableProviderMapsToMissionPauseSignal(t *testing.T) {
	sample, err := (MissionNetMRRSource{}).Observe(context.Background(), "m1", time.Now().UTC())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if sample.Available {
		t.Fatal("expected unavailable sample when provider is unreachable")
	}
}

func TestLiveModeKeyRefusesToLoadWhileImmature(t *testing.T) {
	_, err := NewStripeClient(ClientConfig{
		SecretKey:        "sk_live_not_a_real_secret",
		MaturityCriteria: DefaultMaturityCriteria(),
		MaturityEvidence: MaturityEvidence{},
	})
	if !errors.Is(err, ErrLiveModeKey) {
		t.Fatalf("NewStripeClient live key err=%v, want ErrLiveModeKey", err)
	}
}

func TestNoBillingActionAutoAdmittedBelowTierHWhileImmature(t *testing.T) {
	actions := []BillingChange{
		{Description: "activate subscription for customer"},
		{Description: "change amount", Fields: []string{"amount_cents"}},
		{Description: "issue refund"},
	}
	for _, action := range actions {
		if got := ClassifyBillingChange(action, MaturityImmature); got != admission.TierH {
			t.Fatalf("immature billing action %+v classified %s, want H", action, got)
		}
	}
}
