package notify_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	faketg "github.com/okfriansyah-moh/the-foundry/test/fakes/telegram"
)

// recordingRouter records the command lines routed to it.
type recordingRouter struct {
	mu    sync.Mutex
	calls []string
	reply string
}

func (r *recordingRouter) Handle(_ context.Context, chatID, text string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, chatID+"|"+text)
	return r.reply
}

func (r *recordingRouter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newReceiver(t *testing.T, srv *faketg.Server, offsets notify.OffsetStore, router notify.Router) *notify.Receiver {
	t.Helper()
	sender := &notify.HTTPSender{BaseURL: srv.URL, Token: "test-token", Client: srv.Client()}
	rec, err := notify.NewReceiver(notify.ReceiverConfig{
		BotID: "test-bot", BaseURL: srv.URL, Token: "test-token",
		PollTimeout: 0, Client: srv.Client(),
	}, offsets, router, sender)
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	return rec
}

func TestReceiver_RoutesCommandAndAdvancesOffset(t *testing.T) {
	srv := faketg.New(faketg.DefaultRawLimits())
	defer srv.Close()
	srv.Enqueue(100, 42, "/status wf-1 nonce")

	offsets := notify.NewMemoryOffsetStore()
	router := &recordingRouter{reply: "ok"}
	rec := newReceiver(t, srv, offsets, router)

	n, err := rec.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if n != 1 || router.count() != 1 {
		t.Fatalf("want 1 update routed, got poll=%d calls=%d", n, router.count())
	}
	// Offset advanced to update_id+1 durably.
	off, _ := offsets.GetOffset(context.Background(), "test-bot")
	if off != 101 {
		t.Fatalf("want offset 101, got %d", off)
	}
	// The reply was sent back through sendMessage.
	if srv.Snapshot().Sent != 1 {
		t.Fatalf("want 1 reply sent, got %d", srv.Snapshot().Sent)
	}
}

func TestReceiver_RestartDoesNotReplay(t *testing.T) {
	srv := faketg.New(faketg.DefaultRawLimits())
	defer srv.Close()
	srv.Enqueue(200, 7, "/pause wf-1 nonce")

	offsets := notify.NewMemoryOffsetStore() // simulates the durable Postgres offset
	router := &recordingRouter{reply: "paused"}

	// First daemon lifetime: process the update.
	rec1 := newReceiver(t, srv, offsets, router)
	if _, err := rec1.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce #1: %v", err)
	}
	if router.count() != 1 {
		t.Fatalf("want 1 routed, got %d", router.count())
	}

	// Restart: a brand-new receiver over the same durable offset store must not
	// re-deliver the already-processed update (no replay), and must not lose a
	// new one (no gap).
	rec2 := newReceiver(t, srv, offsets, router)
	if _, err := rec2.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce #2: %v", err)
	}
	if router.count() != 1 {
		t.Fatalf("restart replayed a command: want 1, got %d", router.count())
	}

	// A new update after restart is delivered exactly once.
	srv.Enqueue(201, 7, "/resume wf-1 nonce")
	if _, err := rec2.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce #3: %v", err)
	}
	if router.count() != 2 {
		t.Fatalf("want the post-restart update delivered once: got %d", router.count())
	}
}

// rejectingGuard rejects every chat, standing in for Task 95's ingress limiter.
type rejectingGuard struct{}

func (rejectingGuard) Allow(string) bool { return false }

func TestReceiver_IngressGuardRejectsBeforeRouting(t *testing.T) {
	srv := faketg.New(faketg.DefaultRawLimits())
	defer srv.Close()
	srv.Enqueue(300, 9, "/status wf-1 nonce")

	offsets := notify.NewMemoryOffsetStore()
	router := &recordingRouter{reply: "ok"}
	sender := &notify.HTTPSender{BaseURL: srv.URL, Token: "t", Client: srv.Client()}
	rec, err := notify.NewReceiver(notify.ReceiverConfig{
		BotID: "b", BaseURL: srv.URL, Token: "t", Client: srv.Client(),
		Guard: rejectingGuard{},
	}, offsets, router, sender)
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	if _, err := rec.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if router.count() != 0 {
		t.Fatal("a guard-rejected update must never reach the router")
	}
	// Offset still advances so the rejected update is not re-fetched forever.
	off, _ := offsets.GetOffset(context.Background(), "b")
	if off != 301 {
		t.Fatalf("want offset 301 after rejection, got %d", off)
	}
}

// Ensure the webhook handler compiles against the http.Handler contract.
var _ http.HandlerFunc = (&notify.Receiver{}).WebhookHandler()
