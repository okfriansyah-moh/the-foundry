package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	stripewebhook "github.com/stripe/stripe-go/v79/webhook"
)

type Event struct {
	Name string `json:"name"`
}

type Server struct {
	events []Event
}

func NewServer() *Server { return &Server{} }

func (s *Server) track(name string) {
	s.events = append(s.events, Event{Name: name})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/stripe/checkout-session", func(w http.ResponseWriter, _ *http.Request) {
		s.track("checkout_session_create")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "cs_test_stub"})
	})
	mux.HandleFunc("/stripe/webhook", func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("Stripe-Signature")
		if sig == "" {
			http.Error(w, "missing stripe signature", http.StatusUnauthorized)
			return
		}
		secret := os.Getenv("STRIPE_WEBHOOK_SECRET")
		if secret == "" {
			http.Error(w, "stripe webhook secret not configured", http.StatusServiceUnavailable)
			return
		}
		payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "stripe webhook body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if _, err := stripewebhook.ConstructEvent(payload, sig, secret); err != nil {
			http.Error(w, "invalid stripe signature", http.StatusUnauthorized)
			return
		}
		s.track("stripe_webhook")
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
