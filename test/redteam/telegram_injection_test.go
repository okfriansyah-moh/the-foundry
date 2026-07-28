//go:build redteam

package redteam

import (
	"context"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

func TestTelegramInjection_CrossChatNonceTheftRejected(t *testing.T) {
	chats := notify.NewChatRegistry()
	chats.Register("chat-1", "one")
	chats.Register("chat-2", "two")
	nonces := notify.NewNonceRegistry()
	nonce, err := nonces.Issue("chat-1", "wf-1")
	if err != nil {
		t.Fatalf("issue nonce: %v", err)
	}
	router := &notify.CommandRouter{Chats: chats, Nonces: nonces, Controller: nopController{}}
	reply := router.Handle(context.Background(), "chat-2", "/status wf-1 "+nonce)
	if !strings.Contains(reply, "nonce") {
		t.Fatalf("expected nonce rejection, got %q", reply)
	}
}
