package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SendResult is one Sender.Send outcome.
type SendResult struct {
	// OK is true when Telegram accepted the message.
	OK bool
	// Retryable is true when the failure is transient
	// (docs/foundry/docs/operations/telegram.md §19.17's retryable set:
	// 429/500/502/503/504/network errors). False for the non_retryable
	// set (invalid-token, bot-blocked-by-user, chat-not-found, forbidden,
	// malformed-message) — those go straight to the dead-letter path.
	Retryable bool
	// RetryAfter is Telegram's authoritative backoff duration from a 429
	// response's parameters.retry_after (§19.17), zero if not present.
	RetryAfter time.Duration
	// Err describes the failure for logging/last_error.
	Err error
}

// Sender delivers one Telegram message. HTTPSender is the real
// implementation; test/fakes/telegram provides a mock server HTTPSender
// can point at, and the soak test/unit tests use that mock rather than
// live Telegram credentials.
type Sender interface {
	Send(ctx context.Context, chatID, text string) SendResult
}

// telegramAPIResponse is the subset of Telegram's Bot API JSON envelope
// this client reads (docs/foundry/docs/operations/telegram.md §19.17).
type telegramAPIResponse struct {
	OK         bool `json:"ok"`
	ErrorCode  int  `json:"error_code"`
	Parameters struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
	Description string `json:"description"`
}

// HTTPSender calls the real Telegram Bot API sendMessage method (or a
// mock server presenting the same shape, via BaseURL).
type HTTPSender struct {
	// BaseURL defaults to https://api.telegram.org when empty — tests and
	// the soak harness point this at test/fakes/telegram's mock server.
	BaseURL string
	Token   string
	Client  *http.Client
}

func (h *HTTPSender) baseURL() string {
	if h.BaseURL != "" {
		return h.BaseURL
	}
	return "https://api.telegram.org"
}

func (h *HTTPSender) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return http.DefaultClient
}

// Send posts text to chatID via sendMessage. ctx bounds the HTTP call —
// callers propagate their own cancellation/timeout through it.
func (h *HTTPSender) Send(ctx context.Context, chatID, text string) SendResult {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", h.baseURL(), h.Token)
	body, err := json.Marshal(map[string]string{"chat_id": chatID, "text": text})
	if err != nil {
		return SendResult{Retryable: false, Err: fmt.Errorf("notify: marshal sendMessage body: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SendResult{Retryable: false, Err: fmt.Errorf("notify: build sendMessage request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client().Do(req)
	if err != nil {
		// Network-level failure (timeout, connection reset) — retryable
		// per §19.17's retryable set.
		return SendResult{Retryable: true, Err: fmt.Errorf("notify: sendMessage request: %w", err)}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		return SendResult{OK: true}
	}

	var parsed telegramAPIResponse
	_ = json.Unmarshal(respBody, &parsed)

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := time.Duration(parsed.Parameters.RetryAfter) * time.Second
		return SendResult{
			Retryable:  true,
			RetryAfter: retryAfter,
			Err:        fmt.Errorf("notify: telegram 429: %s", parsed.Description),
		}
	}

	if isRetryableStatus(resp.StatusCode) {
		return SendResult{Retryable: true, Err: fmt.Errorf("notify: telegram %d: %s", resp.StatusCode, parsed.Description)}
	}

	return SendResult{Retryable: false, Err: fmt.Errorf("notify: telegram %d (non-retryable): %s", resp.StatusCode, parsed.Description)}
}

// isRetryableStatus classifies 5xx as retryable per §19.17's retryable
// set (429/500/502/503/504); everything else defaults to non-retryable
// (invalid-token, forbidden, chat-not-found, malformed-message are all
// 4xx in Telegram's Bot API).
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
