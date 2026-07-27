package observe

import (
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

// RateLimitRejections counts requests an ingress surface's Limiter turned
// away, by surface and key (principal or IP) — the token-bucket half of
// docs/PLAN.md Task 33's Steps ("API+webhook rate limits (token bucket
// per principal/IP)").
var RateLimitRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "foundry_rate_limit_rejected_total",
	Help: "Count of ingress requests rejected by a per-principal/IP token-bucket rate limiter, by surface (docs/PLAN.md Task 33).",
}, []string{"surface"})

// IntakeQueueRejections counts requests an IntakeQueue rejected because it
// was at capacity — the "reject-with-429 over silent growth" half of
// Task 33's Steps.
var IntakeQueueRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "foundry_intake_queue_rejected_total",
	Help: "Count of ingress requests rejected because a bounded intake queue was at capacity, by queue name (docs/PLAN.md Task 33).",
}, []string{"queue"})

func init() {
	Registry.MustRegister(RateLimitRejections, IntakeQueueRejections)
}

// Limiter is a per-key token bucket: every distinct key (a principal ID or
// a client IP, per this task's Steps) gets its own independent bucket,
// lazily created on first use. It is the generic building block behind
// this file's Middleware; internal/notify.RateLimiter is a separate,
// Telegram-specific instance of the same pattern (per-chat/global buckets)
// and is not reused here — that type's hierarchical global+per-chat check
// has no notification-flood analogue in a plain ingress rate limiter.
type Limiter struct {
	ratePerSecond float64
	burst         int

	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

// NewLimiter constructs a Limiter whose per-key buckets refill at
// ratePerSecond tokens/second up to burst tokens.
func NewLimiter(ratePerSecond float64, burst int) *Limiter {
	return &Limiter{
		ratePerSecond: ratePerSecond,
		burst:         burst,
		buckets:       make(map[string]*rate.Limiter),
	}
}

// Allow reports whether a request keyed by key may proceed right now,
// consuming one token from key's bucket if so.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		b = rate.NewLimiter(rate.Limit(l.ratePerSecond), l.burst)
		l.buckets[key] = b
	}
	l.mu.Unlock()
	return b.Allow()
}

// ErrIntakeQueueFull is returned by IntakeQueue.TryEnqueue once the queue
// is at capacity — callers reject the admission (HTTP 429) rather than
// letting the queue grow without bound.
var ErrIntakeQueueFull = errors.New("observe: intake queue full")

// IntakeQueue is a bounded admission counter: it tracks how many work
// items are currently admitted (in flight) under name, rejecting any
// further admission once depth reaches capacity instead of growing
// silently — Task 33's Steps ("bounded intake queue (reject-with-429 over
// silent growth)"). It does not buffer or transport the work items
// themselves; callers own that, this type only bounds how many may be
// admitted at once and reports the resulting depth via
// observe.SetQueueDepth (the same queue_depth metric family
// internal/notify's outbound queue already populates, per Task
// 31/metrics.go's own doc comment naming this exact reuse).
type IntakeQueue struct {
	name     string
	capacity int

	mu    sync.Mutex
	depth int
}

// NewIntakeQueue constructs an IntakeQueue reporting queue_depth under
// name, bounded at capacity concurrently-admitted items.
func NewIntakeQueue(name string, capacity int) *IntakeQueue {
	q := &IntakeQueue{name: name, capacity: capacity}
	SetQueueDepth(name, 0)
	return q
}

// TryEnqueue admits one work item if depth < capacity, incrementing depth
// and returning nil; otherwise it leaves depth unchanged and returns
// ErrIntakeQueueFull. Every successful TryEnqueue must be paired with a
// later Release.
func (q *IntakeQueue) TryEnqueue() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.depth >= q.capacity {
		IntakeQueueRejections.WithLabelValues(q.name).Inc()
		return ErrIntakeQueueFull
	}
	q.depth++
	SetQueueDepth(q.name, q.depth)
	return nil
}

// Release returns one admitted work item's capacity to the queue. Calling
// Release without a matching prior successful TryEnqueue is a caller bug;
// Release guards against driving depth negative rather than panicking, so
// a duplicate Release degrades to a harmless no-op instead of corrupting
// the metric.
func (q *IntakeQueue) Release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.depth == 0 {
		return
	}
	q.depth--
	SetQueueDepth(q.name, q.depth)
}

// Depth reports the queue's current admitted count.
func (q *IntakeQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.depth
}

// KeyFunc extracts the rate-limit/admission key (principal or IP) from an
// inbound request.
type KeyFunc func(*http.Request) string

// PrincipalOrIP is the default KeyFunc: it keys on the X-Foundry-Principal
// header when set, and falls back to the request's client IP for
// anonymous/webhook ingress — matching this task's Steps verbatim ("token
// bucket per principal/IP").
//
// Security note (OWASP A01, self-review finding): this function trusts
// X-Foundry-Principal at face value. That is only safe once it sits
// behind authentication middleware that sets/overwrites the header itself
// from a verified identity (Task 36's OIDC-protected API) — an
// unauthenticated caller supplying an arbitrary X-Foundry-Principal value
// per request could otherwise rotate keys to defeat this exact rate
// limiter. Until that authentication layer exists, wire Middleware only
// behind a surface that either strips/overwrites this header after
// authenticating, or use PrincipalOrIP's IP fallback alone (pass a keyFunc
// that ignores the header) for genuinely anonymous ingress such as
// webhooks.
func PrincipalOrIP(r *http.Request) string {
	if p := r.Header.Get("X-Foundry-Principal"); p != "" {
		return p
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// AuthenticatedPrincipalFunc extracts a verified principal from r, e.g. by
// checking an already-verified session bearer token. It returns ok=false
// when no verified principal is available for r (unauthenticated/anonymous
// ingress such as a webhook), signaling the caller to fall back.
type AuthenticatedPrincipalFunc func(r *http.Request) (principal string, ok bool)

// PrincipalOrIPWithAuth returns a KeyFunc that prefers a verified principal
// from authFn over the untrusted X-Foundry-Principal header PrincipalOrIP
// reads: it calls authFn(r) first, and only falls back to PrincipalOrIP's
// header-or-IP behavior when authFn reports ok=false. This closes
// PrincipalOrIP's own OWASP A01 self-review note above — once a caller has
// a verified session principal, that verified value keys the rate limiter,
// not a header any unauthenticated request could set to rotate buckets.
func PrincipalOrIPWithAuth(authFn AuthenticatedPrincipalFunc) KeyFunc {
	return func(r *http.Request) string {
		if authFn != nil {
			if p, ok := authFn(r); ok {
				return p
			}
		}
		return PrincipalOrIP(r)
	}
}

// Middleware wraps next with, in order: (1) limiter's per-key token-bucket
// check, (2) queue's bounded-admission check — both surfaced as HTTP 429
// on rejection, never a silently dropped or queued-forever request. queue
// is released once next.ServeHTTP returns, so depth reflects requests
// actually in flight. surface names the ingress surface for the
// foundry_rate_limit_rejected_total label (e.g. "api", "webhook").
func Middleware(surface string, limiter *Limiter, queue *IntakeQueue, keyFunc KeyFunc, next http.Handler) http.Handler {
	if keyFunc == nil {
		keyFunc = PrincipalOrIP
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := keyFunc(r)

		if limiter != nil && !limiter.Allow(key) {
			RateLimitRejections.WithLabelValues(surface).Inc()
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		if queue != nil {
			if err := queue.TryEnqueue(); err != nil {
				http.Error(w, "server at capacity", http.StatusTooManyRequests)
				return
			}
			defer queue.Release()
		}

		next.ServeHTTP(w, r)
	})
}
