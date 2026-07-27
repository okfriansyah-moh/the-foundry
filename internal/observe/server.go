package observe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// DefaultMetricsAddr is the listen address NewMetricsServer uses when
// callers pass an empty addr — port 9090, matching this task's Validation
// command (`curl -s :9090/metrics`).
const DefaultMetricsAddr = ":9090"

// shutdownTimeout bounds how long Serve waits for in-flight scrapes to
// finish once ctx is cancelled.
const shutdownTimeout = 5 * time.Second

// NewMetricsHandler returns the http.Handler serving Registry's metrics in
// the Prometheus exposition format — the `/metrics` route the card's Steps
// name.
func NewMetricsHandler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}

// Serve runs an HTTP server exposing NewMetricsHandler at /metrics on
// addr (DefaultMetricsAddr if empty) until ctx is cancelled, then shuts it
// down gracefully. It is meant to run in its own goroutine from
// cmd/foundryd's main, mirroring worker.Run's own blocking-until-cancelled
// shape.
func Serve(ctx context.Context, addr string) error {
	if addr == "" {
		addr = DefaultMetricsAddr
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", NewMetricsHandler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("observe: serve metrics on %s: %w", addr, err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("observe: shutdown metrics server: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
