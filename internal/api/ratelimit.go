package api

import (
	"net/http"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// rateLimitKeyFunc builds this server's observe.KeyFunc: it prefers the
// verified session principal (the same bearer JWT authorize's
// principalFromRequest checks) over the untrusted X-Foundry-Principal
// header observe.PrincipalOrIP falls back to, per docs/PLAN.md Task 95 —
// closing the OWASP A01 gap PrincipalOrIP's own doc comment names (an
// unauthenticated caller rotating the header to defeat per-principal
// rate limiting).
func (s *Server) rateLimitKeyFunc() observe.KeyFunc {
	return observe.PrincipalOrIPWithAuth(func(r *http.Request) (string, bool) {
		principal, err := principalFromRequest(s.deps.SessionPub, r)
		if err != nil {
			return "", false
		}
		return principal, true
	})
}
