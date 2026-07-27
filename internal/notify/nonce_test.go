package notify

import (
	"testing"
	"time"
)

// TestNonceRegistry_ExpiredNonceRejected uses an injected fake clock
// (only reachable from inside package notify, since NonceRegistry.now
// is unexported) to fast-forward past NonceTTL without a real sleep.
func TestNonceRegistry_ExpiredNonceRejected(t *testing.T) {
	fakeNow := time.Now()
	r := &NonceRegistry{ttl: NonceTTL, now: func() time.Time { return fakeNow }, entries: make(map[string]*nonceEntry)}

	nonce, err := r.Issue("chat-1", "wf-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	fakeNow = fakeNow.Add(NonceTTL + time.Second)
	if err := r.Consume(nonce, "chat-1", "wf-1"); err != ErrNonceExpired {
		t.Fatalf("want ErrNonceExpired after TTL elapses, got %v", err)
	}
}

func TestNonceRegistry_SingleUse(t *testing.T) {
	r := NewNonceRegistry()
	nonce, err := r.Issue("chat-1", "wf-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := r.Consume(nonce, "chat-1", "wf-1"); err != nil {
		t.Fatalf("first consume should succeed: %v", err)
	}
	if err := r.Consume(nonce, "chat-1", "wf-1"); err != ErrNonceAlreadyUsed {
		t.Fatalf("second consume (replay) must return ErrNonceAlreadyUsed, got %v", err)
	}
}

func TestNonceRegistry_MismatchedChatOrWorkflowRejected(t *testing.T) {
	r := NewNonceRegistry()
	nonce, _ := r.Issue("chat-1", "wf-1")

	if err := r.Consume(nonce, "chat-2", "wf-1"); err != ErrNonceMismatch {
		t.Fatalf("wrong chat must return ErrNonceMismatch, got %v", err)
	}
	if err := r.Consume(nonce, "chat-1", "wf-2"); err != ErrNonceMismatch {
		t.Fatalf("wrong workflow must return ErrNonceMismatch, got %v", err)
	}
}

func TestNonceRegistry_UnknownNonceRejected(t *testing.T) {
	r := NewNonceRegistry()
	if err := r.Consume("does-not-exist", "chat-1", "wf-1"); err != ErrUnknownNonce {
		t.Fatalf("want ErrUnknownNonce, got %v", err)
	}
}
