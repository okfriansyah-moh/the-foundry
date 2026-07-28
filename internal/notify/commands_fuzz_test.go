package notify_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

func FuzzCommands(f *testing.F) {
	f.Add("chat-1", "/freeze")
	f.Add("chat-1", "/status wf-1 nonce")
	f.Add("chat-2", "/rollback promo nonce")
	chats := notify.NewChatRegistry()
	chats.Register("chat-1", "one")
	chats.Register("chat-2", "two")
	router := &notify.CommandRouter{Chats: chats, Nonces: notify.NewNonceRegistry(), Controller: &fakeController{}}
	f.Fuzz(func(t *testing.T, chatID, text string) {
		_ = router.Handle(context.Background(), chatID, text)
	})
}
