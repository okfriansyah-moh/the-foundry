package observe

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// EnvTracingEnabled opts foundryd/the foundry CLI into OTel trace export;
// unset (or any value strconv.ParseBool rejects) leaves OTel's global
// TracerProvider as its zero-cost no-op default, per this task's card
// ("OTel SDK wiring ... opt-in").
const EnvTracingEnabled = "FOUNDRY_OTEL_ENABLED"

// EnvOTLPEndpoint names the standard OTel collector endpoint env var
// (e.g. "localhost:4318"). When set with tracing enabled, spans export via
// OTLP/HTTP; otherwise they print to stderr via stdouttrace — a working
// exporter with no external collector required, useful for local
// `make skp-e2e` runs and this task's own manual verification.
const EnvOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"

// noopShutdown is returned by SetupTracing when tracing is not enabled —
// callers can unconditionally defer the returned func without branching
// on whether tracing was actually turned on.
func noopShutdown(context.Context) error { return nil }

// SetupTracing wires an OpenTelemetry SDK TracerProvider for serviceName
// (e.g. "foundryd", "foundry") and installs it as the global provider, iff
// EnvTracingEnabled is truthy. It returns a shutdown func every caller
// must defer — flushing and closing the exporter — and is a no-op both
// when tracing is disabled and when ctx is later cancelled twice.
//
// Call sites elsewhere in this tree (e.g. internal/kernel/activities.go)
// obtain a tracer via otel.Tracer(name), which resolves to whatever
// provider is currently installed globally — the OTel SDK's own no-op
// implementation when this function was never called or tracing is
// disabled, so instrumented call sites carry zero overhead by default and
// need no build tag or feature flag of their own.
func SetupTracing(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	enabled, _ := strconv.ParseBool(os.Getenv(EnvTracingEnabled))
	if !enabled {
		return noopShutdown, nil
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(serviceName),
	))
	if err != nil {
		return noopShutdown, fmt.Errorf("observe: build resource for %s: %w", serviceName, err)
	}

	exporter, err := newSpanExporter(ctx)
	if err != nil {
		return noopShutdown, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// newSpanExporter returns an OTLP/HTTP exporter when EnvOTLPEndpoint is
// set, else a stdouttrace exporter writing to stderr — see EnvOTLPEndpoint's
// doc comment for why a real collector is optional here.
func newSpanExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if endpoint := os.Getenv(EnvOTLPEndpoint); endpoint != "" {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
		if err != nil {
			return nil, fmt.Errorf("observe: build otlp/http exporter for %s: %w", endpoint, err)
		}
		return exporter, nil
	}

	exporter, err := stdouttrace.New(stdouttrace.WithWriter(os.Stderr))
	if err != nil {
		return nil, fmt.Errorf("observe: build stdout span exporter: %w", err)
	}
	return exporter, nil
}

// Tracer returns a trace.Tracer for name, resolved against whatever
// TracerProvider is currently installed globally (real, if SetupTracing
// enabled one; a zero-cost no-op otherwise). Activities call this rather
// than importing go.opentelemetry.io/otel directly, keeping the OTel API
// surface this package's own concern.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
