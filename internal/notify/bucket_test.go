package notify_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

func TestRateLimiter_PerChatBurstExhausted(t *testing.T) {
	limiter := notify.NewRateLimiter(notify.DefaultLimits())

	if !limiter.Allow("chat-1", notify.ChatPrivate) {
		t.Fatal("first send to a fresh private chat must be allowed (burst=1)")
	}
	if limiter.Allow("chat-1", notify.ChatPrivate) {
		t.Fatal("second immediate send to the same private chat must be blocked (burst exhausted, refill <1/s)")
	}
}

func TestRateLimiter_IndependentChatBuckets(t *testing.T) {
	limiter := notify.NewRateLimiter(notify.DefaultLimits())

	if !limiter.Allow("chat-1", notify.ChatPrivate) {
		t.Fatal("chat-1 first send should be allowed")
	}
	if !limiter.Allow("chat-2", notify.ChatPrivate) {
		t.Fatal("chat-2's bucket must be independent of chat-1's exhausted bucket")
	}
}

func TestRateLimiter_GlobalBucketCapsAcrossChats(t *testing.T) {
	limits := notify.DefaultLimits()
	limits.GlobalBurst = 2
	limits.GlobalPerSecond = 0 // no refill during the test window
	limiter := notify.NewRateLimiter(limits)

	if !limiter.Allow("chat-1", notify.ChatPrivate) {
		t.Fatal("send 1 (global token 1/2) should be allowed")
	}
	if !limiter.Allow("chat-2", notify.ChatPrivate) {
		t.Fatal("send 2 (global token 2/2) should be allowed")
	}
	if limiter.Allow("chat-3", notify.ChatPrivate) {
		t.Fatal("send 3 must be blocked: global bucket exhausted even though chat-3's own bucket is fresh")
	}
}

func TestRateLimiter_GroupPerMinuteBucket(t *testing.T) {
	limiter := notify.NewRateLimiter(notify.DefaultLimits())

	if !limiter.Allow("group-1", notify.ChatGroup) {
		t.Fatal("first send to a fresh group must be allowed (burst=1)")
	}
	if limiter.Allow("group-1", notify.ChatGroup) {
		t.Fatal("second immediate send to the same group must be blocked (15/min refill, burst exhausted)")
	}
}
