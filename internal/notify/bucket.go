package notify

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limits sizes RateLimiter's token buckets. Values default to
// docs/notes/telegram-limits.md's verified, safety-margined ceilings —
// see that file for the live-fetched source and the margin applied.
type Limits struct {
	GlobalPerSecond  float64
	GlobalBurst      int
	PrivatePerSecond float64
	PrivateBurst     int
	GroupPerMinute   float64
	GroupBurst       int
}

// DefaultLimits returns docs/notes/telegram-limits.md's verified
// ceilings: 25 msg/s global, 0.80 msg/s per private chat, 15 msg/min per
// group/supergroup.
func DefaultLimits() Limits {
	return Limits{
		GlobalPerSecond:  25,
		GlobalBurst:      25,
		PrivatePerSecond: 0.80,
		PrivateBurst:     1,
		GroupPerMinute:   15,
		GroupBurst:       1,
	}
}

// RateLimiter enforces docs/foundry/docs/operations/telegram.md §19.16's
// hierarchical bucket order: a send is permitted only when both the
// global bucket and the destination chat's own bucket have a token
// available at the same instant.
type RateLimiter struct {
	limits Limits
	global *rate.Limiter

	mu    sync.Mutex
	chats map[string]*rate.Limiter
}

// NewRateLimiter constructs a RateLimiter sized from limits.
func NewRateLimiter(limits Limits) *RateLimiter {
	return &RateLimiter{
		limits: limits,
		global: rate.NewLimiter(rate.Limit(limits.GlobalPerSecond), limits.GlobalBurst),
		chats:  make(map[string]*rate.Limiter),
	}
}

// chatLimiterLocked returns (creating if absent) chatID's own limiter.
// Callers must hold r.mu.
func (r *RateLimiter) chatLimiterLocked(chatID string, chatType ChatType) *rate.Limiter {
	if l, ok := r.chats[chatID]; ok {
		return l
	}
	var l *rate.Limiter
	if chatType == ChatGroup {
		l = rate.NewLimiter(rate.Limit(r.limits.GroupPerMinute/60.0), r.limits.GroupBurst)
	} else {
		l = rate.NewLimiter(rate.Limit(r.limits.PrivatePerSecond), r.limits.PrivateBurst)
	}
	r.chats[chatID] = l
	return l
}

// Allow reports whether a send to chatID may proceed right now, and if
// so consumes one token from both the global bucket and chatID's own
// bucket. The check-then-consume pair is atomic under r.mu so a caller
// never observes (or spends) a global token without also having a chat
// token available, and vice versa — avoiding a torn hierarchical check
// under concurrent callers.
func (r *RateLimiter) Allow(chatID string, chatType ChatType) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	chat := r.chatLimiterLocked(chatID, chatType)
	if r.global.Tokens() < 1 || chat.Tokens() < 1 {
		return false
	}
	now := time.Now()
	if !r.global.AllowN(now, 1) {
		return false
	}
	if !chat.AllowN(now, 1) {
		// Extremely unlikely given the Tokens() pre-check above, but if it
		// happens the global token is spent slightly early rather than
		// double-spent — it self-corrects on the next refill tick and never
		// under-counts the chat bucket.
		return false
	}
	return true
}
