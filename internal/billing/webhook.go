package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v79"
	stripewebhook "github.com/stripe/stripe-go/v79/webhook"
)

const maxWebhookBytes = 1 << 20

// StripeEvent is a durable, signature-verified Stripe webhook event.
type StripeEvent struct {
	ID              string
	Type            string
	Livemode        bool
	StripeCreatedAt time.Time
	ReceivedAt      time.Time
	Payload         json.RawMessage
}

// EventStore persists Stripe events with replay idempotency in Postgres.
type EventStore struct {
	db *sql.DB
}

// NewEventStore wraps db as the durable Stripe event store.
func NewEventStore(db *sql.DB) *EventStore { return &EventStore{db: db} }

// RecordVerified inserts a verified Stripe event. It returns false when the
// event ID already exists, giving replay-idempotency across process restarts.
func (s *EventStore) RecordVerified(ctx context.Context, event stripe.Event, payload []byte) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("billing: Stripe event store DB is required")
	}
	if strings.TrimSpace(event.ID) == "" {
		return false, fmt.Errorf("billing: Stripe event id is required")
	}
	if !json.Valid(payload) {
		return false, fmt.Errorf("billing: Stripe event payload is not valid JSON")
	}
	createdAt := time.Unix(event.Created, 0).UTC()

	const q = `
INSERT INTO stripe_events (id, type, livemode, stripe_created_at, payload, received_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (id) DO NOTHING
RETURNING received_at`

	var receivedAt time.Time
	err := s.db.QueryRowContext(ctx, q, event.ID, string(event.Type), event.Livemode, createdAt, payload).Scan(&receivedAt)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("billing: record Stripe event %s: %w", event.ID, err)
}

// Get loads a persisted Stripe event by ID.
func (s *EventStore) Get(ctx context.Context, id string) (StripeEvent, error) {
	const q = `
SELECT id, type, livemode, stripe_created_at, received_at, payload
FROM stripe_events WHERE id = $1`
	var ev StripeEvent
	err := s.db.QueryRowContext(ctx, q, id).Scan(&ev.ID, &ev.Type, &ev.Livemode, &ev.StripeCreatedAt, &ev.ReceivedAt, &ev.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return StripeEvent{}, fmt.Errorf("billing: Stripe event %s not found", id)
	}
	if err != nil {
		return StripeEvent{}, fmt.Errorf("billing: load Stripe event %s: %w", id, err)
	}
	ev.StripeCreatedAt = ev.StripeCreatedAt.UTC()
	ev.ReceivedAt = ev.ReceivedAt.UTC()
	return ev, nil
}

// Count returns the number of persisted Stripe webhook events.
func (s *EventStore) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM stripe_events`).Scan(&n); err != nil {
		return 0, fmt.Errorf("billing: count Stripe events: %w", err)
	}
	return n, nil
}

// WebhookHandler verifies Stripe webhook signatures and durably records events.
type WebhookHandler struct {
	secret string
	store  *EventStore
}

// NewWebhookHandler constructs a signature-verifying Stripe webhook handler.
func NewWebhookHandler(signingSecret string, store *EventStore) (*WebhookHandler, error) {
	if strings.TrimSpace(signingSecret) == "" {
		return nil, fmt.Errorf("billing: Stripe webhook signing secret is required")
	}
	if store == nil {
		return nil, fmt.Errorf("billing: Stripe event store is required")
	}
	return &WebhookHandler{secret: signingSecret, store: store}, nil
}

// ServeHTTP implements http.Handler.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sig := r.Header.Get("Stripe-Signature")
	if sig == "" {
		http.Error(w, "missing stripe signature", http.StatusUnauthorized)
		return
	}

	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		http.Error(w, "stripe webhook body too large", http.StatusRequestEntityTooLarge)
		return
	}

	event, err := stripewebhook.ConstructEvent(payload, sig, h.secret)
	if err != nil {
		http.Error(w, "invalid stripe signature", http.StatusUnauthorized)
		return
	}
	if _, err := h.store.RecordVerified(r.Context(), event, payload); err != nil {
		http.Error(w, "could not persist stripe event", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
