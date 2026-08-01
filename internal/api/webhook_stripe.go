package api

import "net/http"

func (s *Server) registerPublic(method, pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(method+" "+pattern, handler)
	s.routes = append(s.routes, Route{Method: method, Pattern: pattern})
}

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.deps.StripeWebhook == nil {
		writeError(w, http.StatusServiceUnavailable, "stripe webhook is not configured")
		return
	}
	s.deps.StripeWebhook.ServeHTTP(w, r)
}
