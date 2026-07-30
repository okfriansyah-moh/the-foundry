package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	"github.com/okfriansyah-moh/the-foundry/internal/policy"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// rateLimitSurface names this server's ingress surface for
// observe.Middleware's foundry_rate_limit_rejected_total label (docs/PLAN.md
// Task 95).
const rateLimitSurface = "api"

// Dependencies are the collaborators Server needs. Every field is required
// (see NewServer) — this is a thin HTTP transport, not a place that grows
// its own copies of the CLI's logic.
type Dependencies struct {
	// DB backs the status endpoint's projected-consistency read path
	// (workflow_status_projection / workflow_transitions — Task 14).
	DB *sql.DB
	// TemporalHostPort/TemporalNamespace back the status endpoint's
	// fresh-consistency read path (DescribeWorkflowExecution).
	TemporalHostPort  string
	TemporalNamespace string

	Evidence   evidence.Store
	Profiles   *profile.Store
	Provenance *provenance.Store

	// QueueConfig and DeliverExecutorAllowlist back POST /v1/plans/{id}/deliver
	// (docs/PLAN.md Task 105 / RTC-01): the kernel resolves the task queue via
	// LaneSelector against QueueConfig, and passes DeliverExecutorAllowlist as
	// the resolved executor allowlist. DeliverExecutorAllowlist must be
	// non-empty; an empty one makes StartDelivery refuse (fail-closed, C4).
	QueueConfig              observe.QueueConfig
	DeliverExecutorAllowlist []string

	// ApprovalSigningKey signs an ApprovedPlan when an approver is
	// recorded (mirrors cmd/foundry/plan_approve.go's local Ed25519 key).
	ApprovalSigningKey ed25519.PrivateKey
	// SessionPub verifies the bearer session JWT every route requires
	// (internal/authn.VerifySession).
	SessionPub *ecdsa.PublicKey
	// WebAuthn backs the step-up ceremony endpoints and ApproveHandler's
	// strong-auth check (internal/authn.Service, Task 25).
	WebAuthn *authn.Service

	// Decider answers the per-route PDP authorization question (Task 23's
	// policy.Decider — internal/policy/pdp.OPADecider in production).
	Decider policy.Decider
	// PolicyDigest is the compiler.Resolved digest Decider was bound to at
	// construction; every Request this server builds carries it, so a
	// Decider built against a stale policy generation denies rather than
	// silently evaluating the wrong policy (see internal/policy/pdp).
	PolicyDigest string

	// RateLimiter/IntakeQueue wire docs/PLAN.md Task 33's control-plane
	// self-protection (per-principal/IP token bucket, bounded admission)
	// in front of this server's routes (Task 95). Both are optional and
	// nil-safe — a nil value disables that check, matching
	// observe.Middleware's own nil-tolerant behavior, so existing tests
	// and callers that don't set them are unaffected.
	RateLimiter *observe.Limiter
	IntakeQueue *observe.IntakeQueue

	// Logger records server-side detail for failures whose full text must
	// not be echoed to the client (OWASP A05). Defaults to slog.Default().
	Logger *slog.Logger
}

// Route names one registered (method, pattern) pair, in the same syntax
// net/http.ServeMux and api/openapi.yaml both use ("GET /v1/plans/{id}").
// Server.Routes is the runtime source of truth contract_test.go diffs
// against the spec — an undocumented route is a spec-drift failure, not
// just a review-time concern.
type Route struct {
	Method  string
	Pattern string
}

// Server is foundryd's HTTP API (docs/PLAN.md Task 36). It holds no
// mutable state of its own beyond its Dependencies and the registered
// mux/route list built once at construction.
type Server struct {
	deps         Dependencies
	mux          *http.ServeMux
	routes       []Route
	webauthnHTTP *authn.WebAuthnHTTP

	// handler is s.mux wrapped in observe.Middleware (Task 95) — built once
	// at construction since it closes over s.rateLimitKeyFunc, itself a
	// function of deps.SessionPub. ServeHTTP delegates to this rather than
	// to s.mux directly.
	handler http.Handler
}

// NewServer builds a Server with every route registered and authorized
// per docs/PLAN.md Task 36's "authz via PDP per route" requirement. It
// errors if any required dependency is missing — there is no partially
// wired Server that silently serves some routes unauthenticated.
func NewServer(deps Dependencies) (*Server, error) {
	if deps.DB == nil {
		return nil, fmt.Errorf("api: DB is required")
	}
	if deps.TemporalHostPort == "" {
		return nil, fmt.Errorf("api: TemporalHostPort is required")
	}
	if deps.TemporalNamespace == "" {
		return nil, fmt.Errorf("api: TemporalNamespace is required")
	}
	if deps.Evidence == nil {
		return nil, fmt.Errorf("api: Evidence store is required")
	}
	if deps.Profiles == nil {
		return nil, fmt.Errorf("api: Profiles store is required")
	}
	if deps.Provenance == nil {
		return nil, fmt.Errorf("api: Provenance store is required")
	}
	if len(deps.ApprovalSigningKey) == 0 {
		return nil, fmt.Errorf("api: ApprovalSigningKey is required")
	}
	if deps.SessionPub == nil {
		return nil, fmt.Errorf("api: SessionPub is required")
	}
	if deps.WebAuthn == nil {
		return nil, fmt.Errorf("api: WebAuthn service is required")
	}
	if deps.Decider == nil {
		return nil, fmt.Errorf("api: Decider is required")
	}
	if deps.PolicyDigest == "" {
		return nil, fmt.Errorf("api: PolicyDigest is required")
	}

	s := &Server{
		deps:         deps,
		mux:          http.NewServeMux(),
		webauthnHTTP: &authn.WebAuthnHTTP{SessionPub: deps.SessionPub, Service: deps.WebAuthn},
	}
	s.registerRoutes()
	s.handler = observe.Middleware(rateLimitSurface, deps.RateLimiter, deps.IntakeQueue, s.rateLimitKeyFunc(), s.mux)
	return s, nil
}

// ServeHTTP implements http.Handler. It runs every request through
// observe.Middleware (rate limit + bounded admission, docs/PLAN.md Task 95)
// before it reaches the route mux; both checks are nil-safe no-ops when
// Dependencies.RateLimiter/IntakeQueue are unset.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Routes returns the sorted list of every registered (method, pattern)
// pair. It is the runtime half of the spec-drift contract test.
func (s *Server) Routes() []Route {
	out := append([]Route{}, s.routes...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pattern != out[j].Pattern {
			return out[i].Pattern < out[j].Pattern
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func (s *Server) logger() *slog.Logger {
	if s.deps.Logger != nil {
		return s.deps.Logger
	}
	return slog.Default()
}

// pdpAction is the single PDP action every internal/api route authorizes
// under (see config/policy/rego/authz.rego's "internal/api route
// authorization" block). The resource string carries the per-route detail
// instead — see registerRoutes.
const pdpAction = "api"

// register wraps handler with the PDP authorization middleware and mounts
// it at "METHOD /pattern", recording it in s.routes.
func (s *Server) register(method, pattern string, resource func(*http.Request) string, handler http.HandlerFunc) {
	s.mux.HandleFunc(method+" "+pattern, s.authorize(pdpAction, resource, handler))
	s.routes = append(s.routes, Route{Method: method, Pattern: pattern})
}

func (s *Server) registerRoutes() {
	s.register(http.MethodPost, "/v1/plans", staticResource("plan:submit"), s.handleSubmitPlan)
	s.register(http.MethodPost, "/v1/plans/{id}/approve", pathResource("plan", "approve"), s.handleApprovePlan)
	s.register(http.MethodPost, "/v1/plans/{id}/deliver", pathResource("plan", "deliver"), s.handleDeliverPlan)
	s.register(http.MethodGet, "/v1/missions", staticResource("mission:list"), s.handleListMissions)
	s.register(http.MethodGet, "/v1/missions/{id}", pathResource("mission", "status"), s.handleMissionStatus)
	s.register(http.MethodPost, "/v1/missions/{id}/start", pathResource("mission", "start"), s.handleStartMission)
	s.register(http.MethodPost, "/v1/missions/{id}/resume", pathResource("mission", "resume"), s.handleResumeMission)

	s.register(http.MethodPost, "/v1/webauthn/register/begin", staticResource("webauthn:register"), s.webauthnHTTP.BeginRegistration)
	s.register(http.MethodPost, "/v1/webauthn/register/finish", staticResource("webauthn:register"), s.webauthnHTTP.FinishRegistration)
	s.register(http.MethodPost, "/v1/webauthn/login/begin", staticResource("webauthn:login"), s.webauthnHTTP.BeginLogin)

	s.register(http.MethodGet, "/v1/workflows/{id}/status", pathResource("workflow", "status"), s.handleWorkflowStatus)

	s.register(http.MethodGet, "/v1/evidence/{id}", pathResource("evidence", "read"), s.handleEvidenceShow)
	s.register(http.MethodGet, "/v1/evidence/{id}/verify", pathResource("evidence", "verify"), s.handleEvidenceVerify)

	s.register(http.MethodGet, "/v1/profiles", staticResource("profile:list"), s.handleProfileList)
	s.register(http.MethodPost, "/v1/profiles", staticResource("profile:create"), s.handleProfileCreate)
	s.register(http.MethodGet, "/v1/profiles/{id}", pathResource("profile", "read"), s.handleProfileShow)
}

func staticResource(name string) func(*http.Request) string {
	return func(*http.Request) string { return name }
}

// pathResource builds a "<noun>:<id>:<verb>" resource string from the
// request's "id" path parameter, e.g. "plan:wf-1:approve".
func pathResource(noun, verb string) func(*http.Request) string {
	return func(r *http.Request) string { return noun + ":" + r.PathValue("id") + ":" + verb }
}

// principalCtxKey is the context key authorize binds the authenticated
// principal under, retrieved by handlers via principalFromContext.
type principalCtxKey struct{}

func principalFromContext(ctx context.Context) string {
	p, _ := ctx.Value(principalCtxKey{}).(string)
	return p
}

// authorize enforces this server's two-layer access control on every
// route (OWASP A01): (1) a valid, unexpired session JWT — no route is
// reachable without one — and (2) a PDP Allow decision for (principal,
// action, resource). Neither layer's failure produces a usable request:
// principalFromRequest's error and a PDP deny both end in a client error
// response, never a fallback-allow.
func (s *Server) authorize(action string, resource func(*http.Request) string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := principalFromRequest(s.deps.SessionPub, r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}

		decision, err := s.deps.Decider.Decide(r.Context(), policy.Request{
			Principal:    principal,
			Action:       action,
			Resource:     resource(r),
			Context:      map[string]any{"method": r.Method, "path": r.URL.Path},
			PolicyDigest: s.deps.PolicyDigest,
		})
		if err != nil {
			s.logger().Error("api: pdp decide failed", "action", action, "error", err)
			writeError(w, http.StatusInternalServerError, "authorization check failed")
			return
		}
		if !decision.Allow {
			writeError(w, http.StatusForbidden, decision.Reason)
			return
		}

		ctx := context.WithValue(r.Context(), principalCtxKey{}, principal)
		next(w, r.WithContext(ctx))
	}
}

// principalFromRequest extracts and verifies the bearer session JWT,
// mirroring internal/authn's own (unexported) helper of the same shape —
// duplicated here rather than exported from internal/authn, since it is a
// three-line header parse, not shared business logic.
func principalFromRequest(pub *ecdsa.PublicKey, r *http.Request) (string, error) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return "", fmt.Errorf("api: missing bearer session token")
	}
	return authn.VerifySession(pub, []byte(strings.TrimPrefix(auth, prefix)))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
