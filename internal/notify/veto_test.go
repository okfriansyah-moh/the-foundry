package notify_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

// fakeVetoExecutor implements VetoExecutor for tests.
type fakeVetoExecutor struct {
	rollbackRef string
	err         error
}

func (f *fakeVetoExecutor) ExecuteVeto(_ context.Context, promoID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.rollbackRef, nil
}

// TestVetoCommand_WithinWindow verifies /rollback executes and returns rollback ref.
func TestVetoCommand_WithinWindow(t *testing.T) {
	nonces := notify.NewNonceRegistry()
	chats := notify.NewChatRegistry()
	chats.Register("chat-1", "alice")
	nonce, _ := nonces.Issue("chat-1", "promo-1")

	router := &notify.CommandRouter{
		Chats:  chats,
		Nonces: nonces,
		Veto:   &fakeVetoExecutor{rollbackRef: "sha-before-promo-1"},
	}

	reply := router.Handle(context.Background(), "chat-1", "/rollback promo-1 "+nonce)
	if reply == "" || reply == "unknown command: /rollback" {
		t.Errorf("rollback command not handled; got: %q", reply)
	}
	if !containsStr(reply, "sha-before-promo-1") && !containsStr(reply, "rollback complete") {
		t.Errorf("reply missing rollback confirmation; got: %q", reply)
	}
}

// TestVetoCommand_InvalidNonce verifies bad nonce is rejected.
func TestVetoCommand_InvalidNonce(t *testing.T) {
	nonces := notify.NewNonceRegistry()
	chats := notify.NewChatRegistry()
	chats.Register("chat-1", "alice")

	router := &notify.CommandRouter{
		Chats:  chats,
		Nonces: nonces,
		Veto:   &fakeVetoExecutor{rollbackRef: "sha-x"},
	}

	reply := router.Handle(context.Background(), "chat-1", "/rollback promo-1 bad-nonce")
	if !containsStr(reply, "unknown") && !containsStr(reply, "nonce") {
		t.Errorf("expected nonce error; got: %q", reply)
	}
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
