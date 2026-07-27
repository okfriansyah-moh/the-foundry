package observe_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

func TestLimiter_AllowsBurstThenRejects(t *testing.T) {
	l := observe.NewLimiter(0, 2) // 0/s refill: burst is the whole budget for this test's duration.
	if !l.Allow("alice") {
		t.Fatal("first request for alice should be allowed (burst)")
	}
	if !l.Allow("alice") {
		t.Fatal("second request for alice should be allowed (burst=2)")
	}
	if l.Allow("alice") {
		t.Fatal("third request for alice should be rejected (burst exhausted, no refill)")
	}
}

func TestLimiter_KeysAreIndependent(t *testing.T) {
	l := observe.NewLimiter(0, 1)
	if !l.Allow("alice") {
		t.Fatal("alice's first request should be allowed")
	}
	if l.Allow("alice") {
		t.Fatal("alice's second request should be rejected")
	}
	if !l.Allow("bob") {
		t.Fatal("bob's first request should be allowed independently of alice's bucket")
	}
}

func TestIntakeQueue_RejectsOverCapacity(t *testing.T) {
	q := observe.NewIntakeQueue("test-intake", 2)

	if err := q.TryEnqueue(); err != nil {
		t.Fatalf("1st TryEnqueue: %v", err)
	}
	if err := q.TryEnqueue(); err != nil {
		t.Fatalf("2nd TryEnqueue: %v", err)
	}
	if err := q.TryEnqueue(); err != observe.ErrIntakeQueueFull {
		t.Fatalf("3rd TryEnqueue = %v, want ErrIntakeQueueFull", err)
	}
	if got := q.Depth(); got != 2 {
		t.Fatalf("Depth() = %d, want 2 (capacity, not silently grown)", got)
	}

	q.Release()
	if got := q.Depth(); got != 1 {
		t.Fatalf("Depth() after Release = %d, want 1", got)
	}
	if err := q.TryEnqueue(); err != nil {
		t.Fatalf("TryEnqueue after Release: %v", err)
	}
}

func TestIntakeQueue_ReleaseNeverGoesNegative(t *testing.T) {
	q := observe.NewIntakeQueue("test-intake-release", 1)
	q.Release() // no prior successful TryEnqueue
	if got := q.Depth(); got != 0 {
		t.Fatalf("Depth() after an unmatched Release = %d, want 0 (no negative depth)", got)
	}
}

func TestPrincipalOrIP_PrefersPrincipalHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:1234"
	r.Header.Set("X-Foundry-Principal", "principal-alice")

	if got := observe.PrincipalOrIP(r); got != "principal-alice" {
		t.Fatalf("PrincipalOrIP = %q, want principal-alice", got)
	}
}

func TestPrincipalOrIP_FallsBackToClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:1234"

	if got := observe.PrincipalOrIP(r); got != "203.0.113.5" {
		t.Fatalf("PrincipalOrIP = %q, want 203.0.113.5", got)
	}
}

func TestPrincipalOrIPWithAuth_PrefersVerifiedPrincipalOverHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:1234"
	r.Header.Set("X-Foundry-Principal", "untrusted-header-value")

	keyFunc := observe.PrincipalOrIPWithAuth(func(*http.Request) (string, bool) {
		return "verified-alice", true
	})
	if got := keyFunc(r); got != "verified-alice" {
		t.Fatalf("keyFunc = %q, want verified-alice (verified principal must win over the untrusted header)", got)
	}
}

func TestPrincipalOrIPWithAuth_FallsBackToPrincipalOrIPWhenUnauthenticated(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.6:1234"
	r.Header.Set("X-Foundry-Principal", "header-value")

	keyFunc := observe.PrincipalOrIPWithAuth(func(*http.Request) (string, bool) {
		return "", false
	})
	if got := keyFunc(r); got != "header-value" {
		t.Fatalf("keyFunc = %q, want header-value (falls back to PrincipalOrIP)", got)
	}
}

func TestPrincipalOrIPWithAuth_NilAuthFnFallsBackToPrincipalOrIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:1234"

	keyFunc := observe.PrincipalOrIPWithAuth(nil)
	if got := keyFunc(r); got != "203.0.113.7" {
		t.Fatalf("keyFunc = %q, want 203.0.113.7 (nil authFn falls back to PrincipalOrIP)", got)
	}
}

func TestMiddleware_RateLimitRejectsWith429(t *testing.T) {
	limiter := observe.NewLimiter(0, 1)
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	mw := observe.Middleware("test-surface", limiter, nil, observe.PrincipalOrIP, next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:1"

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, r)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1st request = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, r)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd request = %d, want 429", rec2.Code)
	}
	if calls != 1 {
		t.Fatalf("next handler called %d times, want exactly 1 (rejected request must never reach it)", calls)
	}
}

func TestMiddleware_QueueFullRejectsWith429AndReleasesAfterNext(t *testing.T) {
	queue := observe.NewIntakeQueue("test-mw-intake", 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := observe.Middleware("test-surface", nil, queue, observe.PrincipalOrIP, next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:1"

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, r)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1st request = %d, want 200", rec1.Code)
	}
	if got := queue.Depth(); got != 0 {
		t.Fatalf("queue depth after request completed = %d, want 0 (released)", got)
	}
}

func TestMiddleware_QueueAtCapacityRejects(t *testing.T) {
	queue := observe.NewIntakeQueue("test-mw-intake-full", 1)
	// Occupy the only slot directly, simulating an in-flight request.
	if err := queue.TryEnqueue(); err != nil {
		t.Fatalf("occupy slot: %v", err)
	}
	defer queue.Release()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not be called when the queue is at capacity")
	})
	mw := observe.Middleware("test-surface", nil, queue, observe.PrincipalOrIP, next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, r)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request against a full queue = %d, want 429", rec.Code)
	}
}
