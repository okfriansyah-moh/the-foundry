// Package telegram is a mock Telegram Bot API server for
// docs/PLAN.md Task 30's soak test and unit tests. It presents the
// same sendMessage HTTP shape internal/notify.HTTPSender speaks, and —
// unlike a rubber-stamp fake — independently enforces Telegram's raw
// published rate limits (not internal/notify's own, already
// safety-margined, bucket sizing), so a sizing bug in
// internal/notify/bucket.go would actually surface here as a real 429,
// not be silently absorbed.
//
// Limits enforced (docs/notes/telegram-limits.md, live-verified against
// the Telegram Bots FAQ): 1 msg/s per chat, ~30 msg/s globally. This
// package deliberately implements its own independent token-bucket
// check (not a call into internal/notify.RateLimiter) — it is the
// opposing side of the contract, not a reuse of the code under test.
package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// RawLimits are Telegram's own published ceilings (not Foundry's
// internal, already-margined defaults) — see package doc.
type RawLimits struct {
	GlobalPerSecond  float64
	PerChatPerSecond float64
}

// DefaultRawLimits returns the limits docs/notes/telegram-limits.md
// verified live: ~30 msg/s global, 1 msg/s per chat.
func DefaultRawLimits() RawLimits {
	return RawLimits{GlobalPerSecond: 30, PerChatPerSecond: 1}
}

// Server is an httptest-backed mock of Telegram's sendMessage endpoint.
type Server struct {
	*httptest.Server

	limits RawLimits
	global *rate.Limiter

	mu    sync.Mutex
	chats map[string]*rate.Limiter

	statsMu     sync.Mutex
	sent        int
	rateLimited int

	updatesMu sync.Mutex
	updates   []InboundUpdate
}

// New starts a Server enforcing limits.
func New(limits RawLimits) *Server {
	s := &Server{
		limits: limits,
		global: rate.NewLimiter(rate.Limit(limits.GlobalPerSecond), int(limits.GlobalPerSecond)),
		chats:  make(map[string]*rate.Limiter),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.route))
	return s
}

// route dispatches on the Bot API method suffix so the one fake server presents
// both the outbound sendMessage endpoint and the inbound getUpdates endpoint
// (docs/PLAN.md Task 112 / INT-04).
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/getUpdates"):
		s.handleGetUpdates(w, r)
	default:
		s.handleSendMessage(w, r)
	}
}

// InboundUpdate is one queued inbound update, in the getUpdates JSON shape
// internal/notify.Receiver reads.
type InboundUpdate struct {
	UpdateID int64  `json:"update_id"`
	ChatID   int64  `json:"-"`
	Text     string `json:"-"`
}

// Enqueue makes an update available to the next getUpdates poll. It is how a
// test drives an inbound command into the wired receiver.
func (s *Server) Enqueue(updateID, chatID int64, text string) {
	s.updatesMu.Lock()
	defer s.updatesMu.Unlock()
	s.updates = append(s.updates, InboundUpdate{UpdateID: updateID, ChatID: chatID, Text: text})
}

// handleGetUpdates returns every queued update whose update_id >= the requested
// offset, in Telegram's envelope shape.
func (s *Server) handleGetUpdates(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	s.updatesMu.Lock()
	var result []map[string]any
	for _, u := range s.updates {
		if u.UpdateID < offset {
			continue
		}
		result = append(result, map[string]any{
			"update_id": u.UpdateID,
			"message": map[string]any{
				"text": u.Text,
				"chat": map[string]any{"id": u.ChatID},
			},
		})
	}
	s.updatesMu.Unlock()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (s *Server) chatLimiter(chatID string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.chats[chatID]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(s.limits.PerChatPerSecond), 1)
	s.chats[chatID] = l
	return l
}

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "malformed-message"})
		return
	}

	chat := s.chatLimiter(req.ChatID)
	if !s.global.Allow() || !chat.Allow() {
		s.statsMu.Lock()
		s.rateLimited++
		s.statsMu.Unlock()

		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error_code": 429, "description": "Too Many Requests: retry later",
			"parameters": map[string]any{"retry_after": 1},
		})
		return
	}

	s.statsMu.Lock()
	s.sent++
	s.statsMu.Unlock()

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// Stats is a snapshot of this server's observed traffic.
type Stats struct {
	Sent        int
	RateLimited int
}

// Snapshot returns the current Stats.
func (s *Server) Snapshot() Stats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return Stats{Sent: s.sent, RateLimited: s.rateLimited}
}

// Close stops the underlying httptest.Server. Present for symmetry with
// httptest.Server's own Close, and so callers don't need to reach
// through the embedded field.
func (s *Server) Close() { s.Server.Close() }
