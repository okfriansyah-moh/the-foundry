//go:build redteam

package redteam

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

type nopController struct{}

func (nopController) Status(context.Context, string) (string, error) { return "ok", nil }
func (nopController) Pause(context.Context, string) error            { return nil }
func (nopController) Resume(context.Context, string) error           { return nil }

func TestExecutorBoundary_UnknownCommandRejected(t *testing.T) {
	chats := notify.NewChatRegistry()
	chats.Register("chat-1", "principal")
	router := &notify.CommandRouter{Chats: chats, Nonces: notify.NewNonceRegistry(), Controller: nopController{}}
	if reply := router.Handle(context.Background(), "chat-1", "/approve-and-deploy now"); reply == "" || reply == "approval accepted" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}
