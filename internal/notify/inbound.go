package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// docs/PLAN.md Task 112 (INT-04): the Telegram engine's inbound half. A real
// receiver, wired into foundryd, feeds the existing CommandRouter with the
// getUpdates offset persisted in Postgres so a restart neither loses nor
// replays a command. All inbound text is untrusted data (C11).

// Update is the subset of a Telegram Update this receiver reads.
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

// getUpdatesResponse is the getUpdates envelope.
type getUpdatesResponse struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// IngressGuard is the optional control-plane self-protection seam (Task 95's
// limiter + bounded intake). A nil guard disables the check. Allow returns
// false to reject an inbound update before any parsing.
type IngressGuard interface {
	Allow(key string) bool
}

// Router is the seam the receiver dispatches a command line through. The
// production *CommandRouter satisfies it.
type Router interface {
	Handle(ctx context.Context, chatID, text string) string
}

// Replier sends a reply back to a chat (the outbound Sender satisfies it).
type Replier interface {
	Send(ctx context.Context, chatID, text string) SendResult
}

// ReceiverConfig configures the inbound receiver.
type ReceiverConfig struct {
	// BotID keys the durable offset (a token digest or bot username).
	BotID string
	// BaseURL/Token point at the Telegram Bot API (or the fake server).
	BaseURL string
	Token   string
	// PollTimeout is the getUpdates long-poll timeout in seconds.
	PollTimeout int
	// Client is the HTTP client; defaults to http.DefaultClient.
	Client *http.Client
	// Guard is the optional ingress limiter/bounded-intake gate.
	Guard IngressGuard
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Receiver polls Telegram getUpdates (long-poll) and routes each update through
// the CommandRouter, advancing the durable offset after each processed update.
type Receiver struct {
	cfg     ReceiverConfig
	offsets OffsetStore
	router  Router
	replier Replier
}

// NewReceiver constructs a Receiver. offsets, router and replier are required.
func NewReceiver(cfg ReceiverConfig, offsets OffsetStore, router Router, replier Replier) (*Receiver, error) {
	if offsets == nil || router == nil || replier == nil {
		return nil, fmt.Errorf("notify: receiver requires an offset store, router and replier")
	}
	if cfg.BotID == "" {
		return nil, fmt.Errorf("notify: receiver requires a bot id for durable offset")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 30
	}
	return &Receiver{cfg: cfg, offsets: offsets, router: router, replier: replier}, nil
}

func (r *Receiver) client() *http.Client {
	if r.cfg.Client != nil {
		return r.cfg.Client
	}
	return http.DefaultClient
}

func (r *Receiver) baseURL() string {
	if r.cfg.BaseURL != "" {
		return r.cfg.BaseURL
	}
	return "https://api.telegram.org"
}

// Run long-polls until ctx is cancelled. It resumes from the durable offset, so
// a restart neither loses nor replays a command.
func (r *Receiver) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := r.PollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.cfg.Logger.Error("notify: getUpdates poll", "error", err)
			// Bounded pause before retrying a failed poll.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

// PollOnce performs a single getUpdates call, routes each returned update, and
// advances the durable offset. It returns the number of updates processed. It
// is the unit-testable core of Run.
func (r *Receiver) PollOnce(ctx context.Context) (int, error) {
	offset, err := r.offsets.GetOffset(ctx, r.cfg.BotID)
	if err != nil {
		return 0, err
	}
	updates, err := r.fetch(ctx, offset)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, u := range updates {
		if err := r.processUpdate(ctx, u); err != nil {
			return processed, err
		}
		// Advance the offset to update_id+1 (Telegram's confirm-consumed
		// semantics) durably before the next update, so a crash mid-batch
		// resumes without re-delivering the ones already handled.
		if err := r.offsets.SetOffset(ctx, r.cfg.BotID, u.UpdateID+1); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// processUpdate applies ingress protection and routes one update. An update
// with no message text is acknowledged (offset advanced) but not routed.
func (r *Receiver) processUpdate(ctx context.Context, u Update) error {
	if u.Message == nil || u.Message.Text == "" {
		return nil
	}
	chatID := strconv.FormatInt(u.Message.Chat.ID, 10)
	// Ingress protection: an unknown/over-limit chat is rejected before any
	// command parsing (Task 95 / Task 112 step 5).
	if r.cfg.Guard != nil && !r.cfg.Guard.Allow(chatID) {
		r.cfg.Logger.Warn("notify: inbound update rejected by ingress guard", "chat", chatID)
		return nil
	}
	reply := r.router.Handle(ctx, chatID, u.Message.Text)
	if reply == "" {
		return nil
	}
	if res := r.replier.Send(ctx, chatID, reply); res.Err != nil {
		// A reply-send failure is logged, not fatal: the command already
		// executed and the offset must still advance.
		r.cfg.Logger.Error("notify: send inbound reply", "chat", chatID, "error", res.Err)
	}
	return nil
}

// fetch calls getUpdates with the given offset and long-poll timeout.
func (r *Receiver) fetch(ctx context.Context, offset int64) ([]Update, error) {
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=%d",
		r.baseURL(), r.cfg.Token, offset, r.cfg.PollTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("notify: build getUpdates request: %w", err)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("notify: getUpdates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("notify: read getUpdates body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("notify: getUpdates status %d", resp.StatusCode)
	}
	var parsed getUpdatesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("notify: decode getUpdates: %w", err)
	}
	if !parsed.OK {
		return nil, fmt.Errorf("notify: getUpdates not ok")
	}
	return parsed.Result, nil
}

// WebhookHandler returns an http.Handler for webhook mode: Telegram POSTs a
// single Update, which is routed identically to the long-poll path. The offset
// is still advanced for parity with long-poll restarts.
func (r *Receiver) WebhookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var u Update
		if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&u); err != nil {
			http.Error(w, "bad update", http.StatusBadRequest)
			return
		}
		if err := r.processUpdate(req.Context(), u); err != nil {
			r.cfg.Logger.Error("notify: webhook process", "error", err)
			http.Error(w, "processing error", http.StatusInternalServerError)
			return
		}
		_ = r.offsets.SetOffset(req.Context(), r.cfg.BotID, u.UpdateID+1)
		w.WriteHeader(http.StatusOK)
	}
}
