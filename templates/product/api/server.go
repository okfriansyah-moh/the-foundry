package api

import (
	"encoding/json"
	"net/http"
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
		if r.Header.Get("Stripe-Signature") == "" {
			http.Error(w, "missing stripe signature", http.StatusUnauthorized)
			return
		}
		s.track("stripe_webhook")
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
