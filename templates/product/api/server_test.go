package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v79"
	stripewebhook "github.com/stripe/stripe-go/v79/webhook"
)

func TestHealthEndpoints(t *testing.T) {
	srv := NewServer().Handler()
	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status=%d, want 200", path, rec.Code)
		}
	}
}

func TestStripeWebhookRequiresSignature(t *testing.T) {
	srv := NewServer().Handler()
	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestStripeWebhookVerifiesSignature(t *testing.T) {
	secret := "whsec_template"
	t.Setenv("STRIPE_WEBHOOK_SECRET", secret)
	payload := []byte(fmt.Sprintf(`{"id":"evt_template","object":"event","api_version":%q,"created":%d,"livemode":false,"type":"checkout.session.completed","data":{"object":{"id":"cs_test_template","object":"checkout.session"}}}`, stripe.APIVersion, time.Now().Unix()))

	srv := NewServer().Handler()
	bad := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: payload, Secret: "whsec_wrong", Timestamp: time.Now()})
	badReq := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	badReq.Header.Set("Stripe-Signature", bad.Header)
	badRec := httptest.NewRecorder()
	srv.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("forged status=%d, want 401", badRec.Code)
	}

	good := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: payload, Secret: secret, Timestamp: time.Now()})
	goodReq := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(string(payload)))
	goodReq.Header.Set("Stripe-Signature", good.Header)
	goodRec := httptest.NewRecorder()
	srv.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusNoContent {
		t.Fatalf("valid status=%d, want 204", goodRec.Code)
	}
}
