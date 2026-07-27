package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

func TestHTTPSender_SuccessfulSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	sender := &notify.HTTPSender{BaseURL: srv.URL, Token: "test-token"}
	result := sender.Send(context.Background(), "chat-1", "hello")
	if !result.OK {
		t.Fatalf("want OK, got %+v", result)
	}
}

func TestHTTPSender_429ParsesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error_code": 429, "description": "flood control",
			"parameters": map[string]any{"retry_after": 17},
		})
	}))
	defer srv.Close()

	sender := &notify.HTTPSender{BaseURL: srv.URL, Token: "test-token"}
	result := sender.Send(context.Background(), "chat-1", "hello")
	if result.OK {
		t.Fatal("429 must not be reported as OK")
	}
	if !result.Retryable {
		t.Fatal("429 must be classified retryable per §19.17")
	}
	if result.RetryAfter != 17*time.Second {
		t.Fatalf("want retry_after=17s (authoritative), got %v", result.RetryAfter)
	}
}

func TestHTTPSender_5xxRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "unavailable"})
	}))
	defer srv.Close()

	sender := &notify.HTTPSender{BaseURL: srv.URL, Token: "test-token"}
	result := sender.Send(context.Background(), "chat-1", "hello")
	if !result.Retryable {
		t.Fatal("503 must be classified retryable")
	}
}

func TestHTTPSender_400NonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "chat not found"})
	}))
	defer srv.Close()

	sender := &notify.HTTPSender{BaseURL: srv.URL, Token: "test-token"}
	result := sender.Send(context.Background(), "chat-1", "hello")
	if result.Retryable {
		t.Fatal("400 (e.g. chat-not-found) must be classified non-retryable per §19.17's non_retryable set")
	}
}
