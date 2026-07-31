package authn

import (
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

// TestSessionStore_SingleUse proves the in-memory SessionStore hands a session
// out exactly once — the replay defense the durable Postgres store must also
// preserve across a restart (Task 114 / INT-06).
func TestSessionStore_SingleUse(t *testing.T) {
	store := newMemSessionStore()
	id, err := store.Put(webauthn.SessionData{Challenge: "abc"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := store.Pop(id)
	if !ok || got.Challenge != "abc" {
		t.Fatalf("first Pop should return the session, got ok=%v challenge=%q", ok, got.Challenge)
	}
	if _, ok := store.Pop(id); ok {
		t.Fatal("second Pop must fail — a session is single-use")
	}
}

// TestNewServiceWithSessions_DefaultsWhenNil verifies a nil SessionStore falls
// back to the in-memory single-use store rather than panicking.
func TestNewServiceWithSessions_DefaultsWhenNil(t *testing.T) {
	svc, err := NewServiceWithSessions(&webauthn.Config{
		RPID: "localhost", RPDisplayName: "Foundry", RPOrigins: []string{"http://localhost:8081"},
	}, NewMemUserStore(), nil)
	if err != nil {
		t.Fatalf("NewServiceWithSessions: %v", err)
	}
	if svc.sessions == nil {
		t.Fatal("session store must default to the in-memory store")
	}
}
