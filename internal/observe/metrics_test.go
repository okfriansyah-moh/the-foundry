package observe_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// TestMetricsHandler_ExposesCatalogSubset is this task's own Validation
// command made deterministic and hermetic: instead of curling a live
// process on :9090, it exercises the exact same promhttp handler this
// package serves there and counts lines containing "foundry_", matching
// `curl -s :9090/metrics | grep -c foundry_` >= 8.
func TestMetricsHandler_ExposesCatalogSubset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	observe.NewMetricsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", rec.Code)
	}

	count := 0
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "foundry_") {
			count++
		}
	}
	if count < 8 {
		t.Fatalf("lines containing foundry_ = %d, want >= 8:\n%s", count, rec.Body.String())
	}
}

// TestMetricsHandler_EveryCatalogMetricPresentAtStartup proves each of the
// nine metric families this task's card names is visible before any
// instrumented call site ever fires — a *Vec collector with no created
// label combination emits nothing, so this also proves metrics.go's
// init() pre-registration actually took effect.
func TestMetricsHandler_EveryCatalogMetricPresentAtStartup(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	observe.NewMetricsHandler().ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, name := range []string{
		"foundry_workflow_completion_rate",
		"foundry_evidence_rejection_rate",
		"foundry_retry_rate",
		"foundry_projection_lag_seconds",
		"foundry_queue_depth",
		"foundry_duplicate_side_effect_prevented_total",
		"foundry_external_operation_divergence_total",
		"foundry_cost_per_task_usd",
		"foundry_provider_waiting_time_seconds",
	} {
		if !strings.Contains(body, "# HELP "+name+" ") {
			t.Errorf("missing HELP line for %s", name)
		}
		if !strings.Contains(body, name) {
			t.Errorf("metric %s has no sample in /metrics output", name)
		}
	}
}

func TestRecordWorkflowCompletion_IncrementsLabeledSeries(t *testing.T) {
	before := scrape(t)
	observe.RecordWorkflowCompletion("SUCCEEDED")
	after := scrape(t)

	if !strings.Contains(after, `foundry_workflow_completion_rate{status="SUCCEEDED"}`) {
		t.Fatalf("expected a SUCCEEDED series after RecordWorkflowCompletion, got:\n%s", after)
	}
	if before == after {
		t.Fatalf("expected /metrics output to change after RecordWorkflowCompletion")
	}
}

func TestRecordActivityAttempt_OnlyCountsRetries(t *testing.T) {
	before := retryCount(t, "TestRecordActivityAttempt_OnlyCountsRetries")
	observe.RecordActivityAttempt("TestRecordActivityAttempt_OnlyCountsRetries", 1, "")
	if got := retryCount(t, "TestRecordActivityAttempt_OnlyCountsRetries"); got != before {
		t.Fatalf("first attempt (attempt=1) must not count as a retry: before=%v after=%v", before, got)
	}
	observe.RecordActivityAttempt("TestRecordActivityAttempt_OnlyCountsRetries", 2, "retryable")
	if got := retryCount(t, "TestRecordActivityAttempt_OnlyCountsRetries"); got != before+1 {
		t.Fatalf("attempt=2 must count as exactly one retry: before=%v after=%v", before, got)
	}
}

func TestRecordEvidenceResult_SeparatesAcceptedAndRejected(t *testing.T) {
	before := scrape(t)
	observe.RecordEvidenceResult(false, "verification-failed")
	after := scrape(t)

	if !strings.Contains(after, `foundry_evidence_rejection_rate{reason="verification-failed",result="rejected"}`) {
		t.Fatalf("expected a rejected series with reason label, got:\n%s", after)
	}
	if before == after {
		t.Fatalf("expected /metrics output to change after RecordEvidenceResult")
	}
}

func TestSetQueueDepth_AndSetProjectionLag(t *testing.T) {
	observe.SetQueueDepth("notifications", 42)
	observe.SetProjectionLag(3.5)
	body := scrape(t)

	if !strings.Contains(body, `foundry_queue_depth{queue="notifications"} 42`) {
		t.Fatalf("expected queue_depth=42 for notifications, got:\n%s", body)
	}
	if !strings.Contains(body, "foundry_projection_lag_seconds 3.5") {
		t.Fatalf("expected projection_lag_seconds=3.5, got:\n%s", body)
	}
}

func TestServe_ServesMetricsAndShutsDownOnCancel(t *testing.T) {
	// Port 0 asks the OS for a free port; observe.Serve's fixed-address
	// contract doesn't expose the chosen port back to the caller, so this
	// test only proves cancellation makes Serve return promptly — the
	// handler itself is already covered by TestMetricsHandler_* above.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- observe.Serve(ctx, "127.0.0.1:0") }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error after cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5s of ctx cancellation")
	}
}

func TestSetupTracing_DisabledByDefaultIsNoop(t *testing.T) {
	t.Setenv(observe.EnvTracingEnabled, "")
	shutdown, err := observe.SetupTracing(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("SetupTracing (disabled) returned error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

func TestSetupTracing_EnabledWithoutEndpointUsesStdoutExporter(t *testing.T) {
	t.Setenv(observe.EnvTracingEnabled, "true")
	t.Setenv(observe.EnvOTLPEndpoint, "")
	shutdown, err := observe.SetupTracing(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("SetupTracing (enabled, no endpoint) returned error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func scrape(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	observe.NewMetricsHandler().ServeHTTP(rec, req)
	return rec.Body.String()
}

// retryCount extracts foundry_retry_rate's summed value across every
// series whose activity label equals activity, tolerating the metric not
// existing yet (0).
func retryCount(t *testing.T, activity string) float64 {
	t.Helper()
	body := scrape(t)
	var total float64
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "foundry_retry_rate{") || !strings.Contains(line, `activity="`+activity+`"`) {
			continue
		}
		fields := strings.Fields(line)
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			t.Fatalf("parse retry_rate sample %q: %v", line, err)
		}
		total += v
	}
	return total
}

// TestMain guards against a stray FOUNDRY_OTEL_ENABLED in the ambient
// environment silently changing TestSetupTracing_DisabledByDefaultIsNoop's
// meaning.
func TestMain(m *testing.M) {
	os.Unsetenv(observe.EnvTracingEnabled)
	os.Exit(m.Run())
}
