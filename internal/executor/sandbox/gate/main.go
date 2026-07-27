// Command foundry-egress-gate is the sidecar half of the Task 34 (FND-15)
// executor sandbox's network model: a minimal HTTP CONNECT relay that only
// tunnels to destinations named in an EgressAllowlist
// (config/sandbox-egress-allowlist.yaml). It never terminates or inspects
// TLS content — it reads the CONNECT request's target host:port, checks it
// against the allowlist, and either splices raw bytes between the two ends
// of the tunnel or refuses with 403.
//
// This binary is multi-homed by the caller (internal/executor/sandbox's
// Runner) onto both the sandbox's private "internal" network (no outside
// route at all) and the engine's normal external network — it is the only
// process with a path to the real network, which is what makes the
// allowlist an enforced boundary rather than a convention the sandboxed
// process could ignore (see the sandbox package's doc.go "Network model").
//
// Exec role: infra+security-review (docs/PLAN.md Task 34 / FND-15).
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/sandbox"
)

func main() {
	addr := flag.String("addr", envOr("FOUNDRY_SANDBOX_GATE_ADDR", ":8080"), "address to listen on")
	allowlistPath := flag.String("allowlist", envOr("FOUNDRY_SANDBOX_ALLOWLIST", "/etc/foundry/sandbox-egress-allowlist.yaml"), "path to the egress allowlist YAML")
	// allowPrivate is OFF by default: a second-review pass flagged that the
	// gate trusted DNS resolution of an allowlisted hostname with no check
	// against private/link-local/loopback/cloud-metadata IPs — a DNS-
	// rebinding-style gap (an allowlisted-looking hostname whose DNS
	// answer, legitimate or attacker-controlled, points at internal
	// infrastructure). Production never needs this flag: the one shipped
	// allowlist entry (api.anthropic.com) is a real, third-party-controlled
	// public host. It exists only so this package's own tests — which
	// deliberately point a fake allowlisted hostname at a local mock
	// endpoint via a private/link-local docker-gateway address — can
	// exercise the gate without tripping the check they're not testing.
	allowPrivate := flag.Bool("allow-private-ips", envOr("FOUNDRY_SANDBOX_GATE_ALLOW_PRIVATE_IPS", "") == "1", "DEV/TEST ONLY: skip the private/reserved-IP rejection check")
	flag.Parse()

	allow, err := sandbox.LoadEgressAllowlist(*allowlistPath)
	if err != nil {
		log.Fatalf("foundry-egress-gate: load allowlist: %v", err)
	}

	g := &gate{
		allow:        allow,
		dial:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		resolve:      net.DefaultResolver.LookupIPAddr,
		allowPrivate: *allowPrivate,
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           g,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("foundry-egress-gate: listening on %s, %d allowlisted destination(s)", *addr, len(allow.Allow))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("foundry-egress-gate: serve: %v", err)
	}
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// dialFunc matches net.Dialer.DialContext's shape, so tests can substitute a
// fake dialer without a real network.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// resolveFunc matches net.Resolver.LookupIPAddr's shape, so tests can
// substitute a fake resolver without real DNS.
type resolveFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

// gate is the http.Handler implementing the CONNECT-only relay.
type gate struct {
	allow sandbox.EgressAllowlist
	dial  dialFunc
	// resolve looks up host's IP addresses before dialing, so isPrivateOrReservedIP
	// can reject a rebinding-style target even though host itself matched
	// the allowlist by name. Required (ServeHTTP will panic on a nil
	// resolve, by design — this check must never be silently skipped).
	resolve resolveFunc
	// allowPrivate disables the private/reserved-IP rejection above.
	// DEV/TEST ONLY — see main()'s flag doc comment.
	allowPrivate bool
}

func (g *gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		// Only CONNECT (TLS tunneling) is supported — the allowlist is
		// seeded exclusively with port-443 entries, so plain-HTTP proxying
		// is deliberately unimplemented rather than half-supported.
		http.Error(w, "only CONNECT is supported", http.StatusNotImplemented)
		return
	}

	host, portStr, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "malformed CONNECT target", http.StatusBadRequest)
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		http.Error(w, "malformed CONNECT target port", http.StatusBadRequest)
		return
	}

	if !g.allow.Allows(host, port) {
		log.Printf("foundry-egress-gate: DENY CONNECT %s:%d (not in allowlist)", host, port)
		http.Error(w, "destination not in egress allowlist", http.StatusForbidden)
		return
	}

	if !g.allowPrivate {
		// Defense-in-depth against DNS rebinding: an allowlisted *hostname*
		// (matched above) whose DNS answer — legitimate or
		// attacker-controlled — resolves to internal infrastructure or a
		// cloud metadata endpoint must not be tunneled to just because the
		// name matched. Reject if ANY resolved address is private/
		// reserved, not just the first.
		addrs, err := g.resolve(r.Context(), host)
		if err != nil {
			log.Printf("foundry-egress-gate: DENY CONNECT %s:%d (resolve failed: %v)", host, port, err)
			http.Error(w, "destination could not be resolved", http.StatusBadGateway)
			return
		}
		for _, a := range addrs {
			if isPrivateOrReservedIP(a.IP) {
				log.Printf("foundry-egress-gate: DENY CONNECT %s:%d (resolved to private/reserved IP %s)", host, port, a.IP)
				http.Error(w, "destination resolved to a private/reserved IP", http.StatusForbidden)
				return
			}
		}
	}

	upstream, err := g.dial(r.Context(), "tcp", net.JoinHostPort(host, portStr))
	if err != nil {
		log.Printf("foundry-egress-gate: ALLOW CONNECT %s:%d but dial failed: %v", host, port, err)
		http.Error(w, "upstream dial failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return
	}
	defer client.Close()

	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	log.Printf("foundry-egress-gate: ALLOW CONNECT %s:%d", host, port)

	relay(client, upstream)
}

// relay splices bytes bidirectionally between client and upstream until
// either side closes, at which point both are torn down. Neither side's
// payload is inspected or logged — the gate is a tunnel, not a proxy that
// terminates TLS.
func relay(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(upstream, client)
	go cp(client, upstream)
	<-done
}

// isPrivateOrReservedIP reports whether ip is loopback, link-local
// (unicast or multicast — this covers the 169.254.0.0/16 range, which
// includes the common cloud-provider metadata address
// 169.254.169.254), RFC1918/ULA private space, or unspecified (0.0.0.0/
// ::). Any of these on a CONNECT target that matched the allowlist by
// *name* is treated as a DNS-rebinding attempt, not a legitimate provider
// endpoint — the one shipped allowlist entry (api.anthropic.com) is a real
// public host and will never legitimately resolve here.
func isPrivateOrReservedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified()
}
