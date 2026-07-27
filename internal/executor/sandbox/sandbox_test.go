package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests are gated behind RUN_SANDBOX=1 (docs/PLAN.md Task 34
// Validation) because they need a real container engine — they launch real
// containers and networks and are the whole point of the two-lane CI design
// (Blocker B9): the bare-runner sandbox-tests CI job is the authoritative
// signal; this file also runs, non-authoritatively, in the local
// socket-mount lane described in README.md.
//
// Engine: the tests use whatever FOUNDRY_SANDBOX_TEST_ENGINE names,
// defaulting to "docker". The task card specifies rootless podman (or
// runc); this session's own validation used docker because neither podman
// nor runc were installed here — see oci.go's leading comment for the full
// caveat. Every test here skips cleanly (not a fabricated pass) if the
// named engine binary isn't on PATH.

func testEngine(t *testing.T) string {
	t.Helper()
	engine := os.Getenv("FOUNDRY_SANDBOX_TEST_ENGINE")
	if engine == "" {
		engine = "docker"
	}
	if _, err := exec.LookPath(engine); err != nil {
		t.Skipf("skipping: engine %q not found on PATH: %v", engine, err)
	}
	return engine
}

func testImage() string {
	if v := os.Getenv("FOUNDRY_SANDBOX_TEST_IMAGE"); v != "" {
		return v
	}
	return DefaultImage
}

// requireSandbox skips the test unless RUN_SANDBOX=1 and returns the engine
// binary to use.
func requireSandbox(t *testing.T) string {
	t.Helper()
	if os.Getenv("RUN_SANDBOX") != "1" {
		t.Skip("skipping: set RUN_SANDBOX=1 to run real-container sandbox tests (docs/PLAN.md Task 34)")
	}
	return testEngine(t)
}

// sandboxWritableTempDir returns a fresh temp dir, host-permissioned so the
// sandbox container's own fixed --user 10001:10001 (a different UID than
// whatever user is running `go test`) can actually write into it once
// bind-mounted as /workspace:rw. t.TempDir() alone defaults to 0o700, owned
// solely by the test process's own UID — on real Linux (unlike macOS Docker
// Desktop's permissive bind-mount layer, which silently masked this the
// first time these tests ran anywhere) that leaves UID 10001 with no
// permission to create or modify anything inside it, so every test that
// needs the sandboxed process to write into its own workspace failed with
// "permission denied" the first time this ran on a real Linux CI runner.
// 0o777 is safe here specifically because this directory's entire lifetime
// is a single ephemeral test run under t.TempDir()'s own cleanup — it is
// never a production workspace path (those are the caller/kernel's own
// concern, not this test helper's).
func sandboxWritableTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod test workspace dir %s: %v", dir, err)
	}
	return dir
}

// newTestRunner builds a Runner wired to a real, disposable workspace dir
// and the repo's own shipped allowlist, and returns it already Start()'d,
// with cleanup registered.
func newTestRunner(t *testing.T, engine string) *Runner {
	t.Helper()

	wsHost := sandboxWritableTempDir(t)
	allowlistPath, err := filepath.Abs(filepath.Join("..", "..", "..", "config", "sandbox-egress-allowlist.yaml"))
	if err != nil {
		t.Fatalf("resolve allowlist path: %v", err)
	}
	allow, err := LoadEgressAllowlist(allowlistPath)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist: %v", err)
	}

	cfg := Config{
		Engine:            engine,
		Image:             testImage(),
		WorkspaceHost:     wsHost,
		Allowlist:         allow,
		AllowlistHostPath: allowlistPath,
		Timeout:           60 * time.Second,
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := r.Close(closeCtx); err != nil {
			t.Logf("Close: %v (leaked resources may need manual cleanup: %s, %s)", err, r.gateName(), r.networkName())
		}
	})
	return r
}

// --- (a) three escape-attempt tests: all must be blocked ---

// TestEscape_ReadRestrictedPathBlocked proves the sandbox cannot read
// /etc/shadow: it runs as a non-root user (Config.User, default
// 10001:10001) with no added capabilities, so the container's own
// /etc/shadow — 0640 root:shadow, same as any stock Linux image — is
// unreadable to it. This is the literal "read /etc/shadow path" escape
// attempt named in docs/PLAN.md Task 34's Steps.
func TestEscape_ReadRestrictedPathBlocked(t *testing.T) {
	engine := requireSandbox(t)
	r := newTestRunner(t, engine)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// A nonzero ExitCode with a nil error is this package's normal shape for
	// "the command ran and failed" (mirroring executor.RunSubprocess/
	// CommandResult) — err is reserved for the run itself not completing
	// (start/timeout failures), so the blocked-or-not signal is ExitCode,
	// not err.
	result, err := r.RunCommand(ctx, []string{"cat", "/etc/shadow"}, 0)
	if err != nil {
		t.Fatalf("RunCommand: %v (%+v)", err, result)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected nonzero exit reading /etc/shadow, got 0: %+v", result)
	}
	t.Logf("blocked as expected: exit=%d stderr=%q", result.ExitCode, strings.TrimSpace(result.Stderr))
}

// TestEscape_EgressToDisallowedHostBlocked proves the sandbox's network has
// no route to a host that isn't the gate — the run network is created
// --internal, so this holds regardless of whether the process inside
// bothers to honor HTTPS_PROXY.
func TestEscape_EgressToDisallowedHostBlocked(t *testing.T) {
	engine := requireSandbox(t)
	r := newTestRunner(t, engine)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// example.com is not in config/sandbox-egress-allowlist.yaml.
	result, err := r.RunCommand(ctx, []string{"curl", "-sS", "--max-time", "5", "https://example.com"}, 10*time.Second)
	if err != nil {
		t.Fatalf("RunCommand: %v (%+v)", err, result)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected nonzero curl exit for disallowed egress, got 0: %+v", result)
	}
	t.Logf("blocked as expected: exit=%d stderr=%q", result.ExitCode, strings.TrimSpace(result.Stderr))
}

// TestEscape_WriteOutsideWorkspaceBlocked proves the sandbox's rootfs is
// read-only (plus the read-only cache mount), so only the workspace bind
// mount itself accepts writes.
func TestEscape_WriteOutsideWorkspaceBlocked(t *testing.T) {
	engine := requireSandbox(t)
	r := newTestRunner(t, engine)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := r.RunCommand(ctx, []string{"sh", "-c", "touch /etc/foundry-escape-attempt"}, 0)
	if err != nil {
		t.Fatalf("RunCommand: %v (%+v)", err, result)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected nonzero exit writing outside workspace, got 0: %+v", result)
	}
	t.Logf("blocked as expected: exit=%d stderr=%q", result.ExitCode, strings.TrimSpace(result.Stderr))

	// Control: the workspace itself must remain writable — otherwise this
	// "escape blocked" result would be indistinguishable from "everything
	// is broken."
	result2, err := r.RunCommand(ctx, []string{"sh", "-c", "touch /workspace/proof-of-write && echo ok"}, 0)
	if err != nil {
		t.Fatalf("expected workspace to remain writable, got error: %v (%+v)", err, result2)
	}
	if strings.TrimSpace(result2.Stdout) != "ok" {
		t.Fatalf("expected workspace write to succeed, got %+v", result2)
	}
}

// --- (b) legitimate-egress test: the allowlist actually grants ---

// mockProvider is a real TLS server standing in for "the configured
// executor's own LLM provider endpoint" — bound to a fixed port on all
// interfaces on the docker host so the gate can reach it via Docker
// Desktop's host-gateway address (Config.GateExtraHosts). Host is
// deliberately fake (never a real provider name).
type mockProvider struct {
	host string
	port int
}

func startMockProvider(t *testing.T, body string) mockProvider {
	t.Helper()
	const host = "foundry-sandbox-mock-provider.test"
	const port = 18443

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		t.Skipf("skipping: could not bind fixed test port %d: %v", port, err)
	}
	mock := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	mock.Listener.Close()
	mock.Listener = ln
	mock.StartTLS()
	t.Cleanup(mock.Close)

	return mockProvider{host: host, port: port}
}

// runnerAgainstMock builds a Runner whose allowlist and GateExtraHosts are
// wired to exactly one destination: mp. Start()'d and Close()'d via
// t.Cleanup.
func runnerAgainstMock(t *testing.T, engine string, mp mockProvider, wsHost, reason string) *Runner {
	t.Helper()
	allowlistPath := filepath.Join(t.TempDir(), "allowlist.yaml")
	if err := os.WriteFile(allowlistPath, []byte(fmt.Sprintf(`
version: 1
allow:
  - host: %s
    port: %d
    reason: %q
`, mp.host, mp.port, reason)), 0o600); err != nil {
		t.Fatalf("write test allowlist: %v", err)
	}
	allow, err := LoadEgressAllowlist(allowlistPath)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist: %v", err)
	}

	cfg := Config{
		Engine:            engine,
		Image:             testImage(),
		WorkspaceHost:     wsHost,
		Allowlist:         allow,
		AllowlistHostPath: allowlistPath,
		GateExtraHosts:    []string{mp.host + ":host-gateway"},
		// Test-only: the mock provider is deliberately reached via a
		// private/link-local docker-gateway address (GateExtraHosts
		// above), which the gate's DNS-rebinding defense (gate/main.go's
		// isPrivateOrReservedIP) would otherwise correctly reject.
		// Production wiring must never set this.
		GateAllowPrivateIPs: true,
		Timeout:             60 * time.Second,
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = r.Close(closeCtx)
	})
	return r
}

// TestLegitimateEgress_AllowlistedDestinationSucceeds proves a request from
// inside the sandbox to an allowlisted destination succeeds through the
// gate — the allowlist grants, not just denies (docs/PLAN.md Task 34
// Steps: "one legitimate-egress test"). The client uses curl -k: this test
// proves the gate's allowlist grants a real TLS tunnel end-to-end (CONNECT
// allowed, bytes relayed, TLS handshake completes), not that the mock
// cert's SAN happens to match — cert/hostname validation is the calling
// application's concern (and, for a real provider, a real CA-issued cert),
// not something the gate inspects since it never terminates TLS.
func TestLegitimateEgress_AllowlistedDestinationSucceeds(t *testing.T) {
	engine := requireSandbox(t)
	mp := startMockProvider(t, "mock-provider-ok")
	r := runnerAgainstMock(t, engine, mp, sandboxWritableTempDir(t), "test-only mock provider endpoint")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := r.RunCommand(ctx, []string{
		"curl", "-sS", "-k", "--max-time", "10",
		fmt.Sprintf("https://%s:%d/", mp.host, mp.port),
	}, 15*time.Second)
	if err != nil {
		t.Fatalf("expected allowlisted egress to succeed, got error: %v (%+v)", err, result)
	}
	if !strings.Contains(result.Stdout, "mock-provider-ok") {
		t.Fatalf("expected mock provider response, got %+v", result)
	}
}

// TestClaudeCodeAdapter_FunctionalInsideSandbox is the card's
// claude-code-specific Acceptance line: "claude-code adapter functional
// inside sandbox using only the allowlisted endpoint, no cache-related
// network calls." It does not modify internal/executor/claudecode (out of
// this task's Outputs); instead it runs a stub standing in for the `claude`
// CLI — same shape as internal/executor/claudecode's own test stubs (a
// script that reads a prompt and emits `--output-format json`-shaped
// output) — inside a real sandbox container, and proves it can still reach
// its one allowlisted provider endpoint from behind the same default-deny
// network the escape tests above prove has no other route out. The
// "no cache-related network calls" half of the Acceptance line follows from
// TestEscape_EgressToDisallowedHostBlocked plus DefaultCacheMounts (oci.go):
// package resolution is served entirely from the read-only cache mounts,
// which are local bind/volume mounts, not a network fetch — there is no
// separate network path for caches to accidentally use.
func TestClaudeCodeAdapter_FunctionalInsideSandbox(t *testing.T) {
	engine := requireSandbox(t)
	mp := startMockProvider(t, `{"result":"hello world","is_error":false}`)

	wsHost := sandboxWritableTempDir(t)
	stub := "#!/bin/sh\n" +
		"set -e\n" +
		"cat > /workspace/prompt-received.txt\n" +
		fmt.Sprintf("curl -sS -k --max-time 5 https://%s:%d/\n", mp.host, mp.port)
	if err := os.WriteFile(filepath.Join(wsHost, "claude-stub.sh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write claude stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsHost, "prompt.md"), []byte("# Task\n\nReply hello world.\n"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	r := runnerAgainstMock(t, engine, mp, wsHost, "claude-code's own LLM provider endpoint (test stub)")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := r.RunCommand(ctx, []string{
		"sh", "-c", "sh /workspace/claude-stub.sh < /workspace/prompt.md",
	}, 15*time.Second)
	if err != nil {
		t.Fatalf("expected the claude-code stub to run inside the sandbox, got error: %v (%+v)", err, result)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected the claude-code stub to exit 0, got %+v", result)
	}
	if !strings.Contains(result.Stdout, `"result":"hello world"`) {
		t.Fatalf("expected the stub's provider call to reach the mock and echo its body, got %+v", result)
	}
}

// --- real-workload test: fix for a second-review CRITICAL finding ---

// TestRealWorkload_GoBuildUsesWritableCacheMounts is the direct regression
// test for a second-review finding: the original DefaultCacheMounts pointed
// GOBUILD's cache at /root/.cache/go-build, but the sandbox image's actual
// non-root user's $HOME is /home/sandbox, so Go's GOCACHE default never
// resolved to that mount — a real `go build ./...` failed with "mkdir
// /home/sandbox/.cache: read-only file system", reproduced in this session
// before the fix. This test runs a real `go build` (not just
// cat/curl/touch/sh, which is all the original escape/legitimate-egress
// tests exercised) against two freshly created, uniquely named cache
// volumes — proving the fix from a cold start, not a pre-warmed one: the
// mount path now matches where GOCACHE is explicitly pointed (an env var,
// not a $HOME-derived default), the mount is read-write, and Start's
// cache-ownership fix-up (buildCacheChownArgs) makes that freshly-created,
// otherwise root-owned volume writable by the non-root sandbox user.
func TestRealWorkload_GoBuildUsesWritableCacheMounts(t *testing.T) {
	engine := requireSandbox(t)

	id, err := randomID()
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	gomodVol := "foundry-sbx-test-gomod-" + id
	gobuildVol := "foundry-sbx-test-gobuild-" + id
	t.Cleanup(func() {
		_ = exec.Command(engine, "volume", "rm", "-f", gomodVol, gobuildVol).Run()
	})

	wsHost := sandboxWritableTempDir(t)
	if err := os.WriteFile(filepath.Join(wsHost, "go.mod"), []byte("module example.com/sandboxtest\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsHost, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	allowlistPath := filepath.Join(t.TempDir(), "allowlist.yaml")
	if err := os.WriteFile(allowlistPath, []byte("version: 1\nallow:\n  - host: api.anthropic.com\n    port: 443\n"), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	allow, err := LoadEgressAllowlist(allowlistPath)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist: %v", err)
	}

	cfg := Config{
		Engine:            engine,
		Image:             testImage(),
		WorkspaceHost:     wsHost,
		Allowlist:         allow,
		AllowlistHostPath: allowlistPath,
		CacheMounts: []CacheMount{
			{Kind: CacheKindGoMod, Target: "/go/pkg/mod", Source: gomodVol},
			{Kind: CacheKindGoBuild, Target: "/home/sandbox/.cache/go-build", Source: gobuildVol},
		},
		Timeout: 60 * time.Second,
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = r.Close(closeCtx)
	})

	// Prove GOCACHE/GOMODCACHE actually resolve to the mounted paths, not
	// $HOME-derived defaults — direct evidence, not an inference from
	// `go build` merely succeeding.
	envResult, err := r.RunCommand(ctx, []string{"go", "env", "GOCACHE", "GOMODCACHE"}, 15*time.Second)
	if err != nil || envResult.ExitCode != 0 {
		t.Fatalf("go env: err=%v result=%+v", err, envResult)
	}
	wantEnv := "/home/sandbox/.cache/go-build\n/go/pkg/mod\n"
	if envResult.Stdout != wantEnv {
		t.Fatalf("go env GOCACHE/GOMODCACHE = %q, want %q", envResult.Stdout, wantEnv)
	}

	// The actual regression test: a real `go build` against a freshly
	// created (previously root-owned, per this session's own reproduction)
	// cache volume must succeed, not fail on cache-directory creation.
	buildResult, err := r.RunCommand(ctx, []string{"go", "build", "-o", "/workspace/out", "./..."}, 60*time.Second)
	if err != nil {
		t.Fatalf("go build: %v (%+v)", err, buildResult)
	}
	if buildResult.ExitCode != 0 {
		t.Fatalf("expected `go build` to succeed inside the sandbox, got %+v", buildResult)
	}

	if _, err := os.Stat(filepath.Join(wsHost, "out")); err != nil {
		t.Fatalf("expected go build's output binary in the workspace: %v", err)
	}

	// A second, independent RunCommand call (a fresh container, same
	// volumes) reusing the now-populated GOBUILD cache proves the fix
	// isn't a one-shot fluke of container-local state.
	rebuildResult, err := r.RunCommand(ctx, []string{"go", "build", "-o", "/workspace/out2", "./..."}, 60*time.Second)
	if err != nil || rebuildResult.ExitCode != 0 {
		t.Fatalf("second go build (cache reuse across containers): err=%v result=%+v", err, rebuildResult)
	}
}

// --- real-workload test: fix for a second-review MEDIUM (#4) finding ---

// TestEscape_AllowlistedHostnameRebindingToPrivateIPBlocked is the direct,
// real-container regression test for a second-review finding: an
// allowlisted *hostname* whose DNS answer resolves to a private/reserved IP
// must still be rejected by the gate (DNS-rebinding defense), even though
// the name itself matched the allowlist. Unlike gate/main_test.go's
// fake-resolver unit tests, this exercises the real gate binary, real DNS
// resolution (via --add-host), and the real CONNECT path end-to-end.
func TestEscape_AllowlistedHostnameRebindingToPrivateIPBlocked(t *testing.T) {
	engine := requireSandbox(t)

	const rebindHost = "foundry-sandbox-rebind-test.test"
	allowlistPath := filepath.Join(t.TempDir(), "allowlist.yaml")
	if err := os.WriteFile(allowlistPath, []byte(fmt.Sprintf(`
version: 1
allow:
  - host: %s
    port: 443
    reason: test-only, deliberately made to resolve to a private IP
`, rebindHost)), 0o600); err != nil {
		t.Fatalf("write test allowlist: %v", err)
	}
	allow, err := LoadEgressAllowlist(allowlistPath)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist: %v", err)
	}

	cfg := Config{
		Engine:            engine,
		Image:             testImage(),
		WorkspaceHost:     sandboxWritableTempDir(t),
		Allowlist:         allow,
		AllowlistHostPath: allowlistPath,
		// rebindHost matches the allowlist by name, but resolves (via
		// --add-host) to a private, non-routable IP — GateAllowPrivateIPs
		// is deliberately left false (the production default) so the
		// gate's rejection check is actually exercised, not bypassed.
		GateExtraHosts: []string{rebindHost + ":10.255.255.1"},
		Timeout:        60 * time.Second,
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = r.Close(closeCtx)
	})

	result, err := r.RunCommand(ctx, []string{
		"curl", "-sS", "-k", "--max-time", "10", "https://" + rebindHost + "/",
	}, 15*time.Second)
	if err != nil {
		t.Fatalf("RunCommand: %v (%+v)", err, result)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected the rebinding target to be blocked, got success: %+v", result)
	}
	t.Logf("blocked as expected: exit=%d stderr=%q", result.ExitCode, strings.TrimSpace(result.Stderr))
}
