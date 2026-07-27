// Validation note (docs/PLAN.md Task 34 / FND-15, updated by Task 97 /
// FND-15R): Task 34's own sessions ran RUN_SANDBOX=1 tests against `docker`
// as Config.Engine, not rootless podman/runc — neither was installed in that
// execution environment, and nested rootless-podman-in-podman was not
// attempted (the task's own Blocker-B9 resolution explicitly rejects a
// privileged nested-daemon path for the same reason). `docker` was the
// closest available engine exposing the same run/network CLI surface this
// package depends on, so it stood in for the production engine to prove the
// *topology* (internal network + gate sidecar + read-only rootfs + dropped
// caps + non-root user) actually works, not to claim rootless-podman itself
// was exercised. The bare-runner CI lane (.github/workflows/ci.yaml's
// sandbox-tests job) uses plain docker and remains required/unchanged.
//
// Task 97 (FND-15R) adds a second, sibling bare-runner CI job,
// sandbox-tests-rootless, that installs real rootless podman and re-runs
// this same test suite via FOUNDRY_SANDBOX_TEST_ENGINE=podman, plus a new
// rootless_test.go that inspects the host-side UID owning the container
// process to prove genuine user-namespace remapping (not merely the
// in-container non-root UID this package already proved). That job is this
// package's actual rootless-verification lane going forward; see
// rootless_test.go's own leading comment for exactly what it proves and its
// negative control.
package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
)

// EnvSandboxMode is the environment variable an executor harness reads to
// decide whether to route a task through this package instead of
// executor.RunSubprocess directly. This package never reads it itself — the
// harness that constructs a Runner does, keeping the "which mode" decision
// out of the sandbox implementation.
const EnvSandboxMode = "FOUNDRY_SANDBOX"

// ModeOCI is the value of EnvSandboxMode that selects this package.
const ModeOCI = "oci"

// DefaultImage is the image tag built from deploy/images/executor.Dockerfile
// — the `foundry-executor-sandbox` lineage named in CLAUDE.md's container
// topology table (owner: this task; the only other place a 5th lineage
// could sneak in is here, so this is the single name every caller should
// use rather than inventing a local tag).
const DefaultImage = "foundry-executor-sandbox:latest"

// defaultEngine is the container CLI shelled out to. The task card
// specifies rootless podman (or runc); docker is accepted unchanged because
// this package only depends on a docker-CLI-compatible run/network surface
// (see this file's leading validation-note comment).
//
// HONESTY NOTE ON "ROOTLESS" (docs/PLAN.md Task 34, updated by Task 97):
// this package's in-container isolation (never root inside the container:
// --user 10001:10001, --cap-drop=ALL, no-new-privileges — verified, tested,
// real) is independent from and must not be confused with engine-level
// rootlessness (the container ENGINE itself running without root privilege
// on the host via user-namespace UID remapping, which is what "rootless
// podman" specifically names). Task 34's own sessions never had rootless
// podman or runc available to exercise this — every test run through that
// task, including its bare-runner CI job, used standard (non-rootless)
// `docker`. Task 97 adds the actual verification lane for this property:
// a sibling CI job (sandbox-tests-rootless) that installs real rootless
// podman and a dedicated test (rootless_test.go's
// TestRootless_ContainerProcessRunsUnderUnprivilegedHostUID) that inspects
// the host-side UID owning the container process — proving user-namespace
// remapping, not just in-container non-root. As of this task's own
// implementation session, that CI job's green result has not yet been
// observed on a real GitHub Actions runner (no rootless podman was
// available in this session's own execution environment either) — see
// docs/PLAN.md Task 97's Status line for the precise, current verification
// state. Do not read "rootless" as verified anywhere in this package until
// that job has actually run green on a real runner.
const defaultEngine = "podman"

const (
	defaultWorkspaceTarget = "/workspace"
	defaultCPULimit        = "2"
	defaultMemoryLimit     = "2g"
	defaultPIDsLimit       = 256
	defaultUser            = "10001:10001"
	defaultTimeout         = 30 * time.Minute
	gatePort               = 8080
	gateImageAllowlistPath = "/etc/foundry/sandbox-egress-allowlist.yaml"
	// externalNetwork is Docker Engine's own default bridge network name.
	// Podman's default network (netavark or CNI) is named "podman", not
	// "bridge" — see externalNetworkName, whose per-engine defaulting is the
	// actual value Start() uses.
	externalNetwork = "bridge"
	// externalNetworkPodman is rootless (and rootful) Podman's default
	// network name, created automatically on first `podman network ls` /
	// `podman run` the same way Docker auto-creates "bridge". Docs/PLAN.md
	// Task 97's rootless-podman verification lane is the first lane to ever
	// actually run this package's Runner.Start against real podman; a
	// hardcoded "bridge" would fail outright there with "network not found"
	// — a genuine compatibility gap that predates Task 97 (Task 34 already
	// defaulted Config.Engine to "podman" but only ever validated against
	// docker), caught while wiring this task's rootless test lane.
	externalNetworkPodman = "podman"
)

// CacheKind identifies which Go cache a CacheMount represents, so
// buildSandboxRunArgs can both mount it in the right read/write mode and
// point the matching go env var (GOMODCACHE/GOCACHE) at its Target
// explicitly — never relying on Go's own $HOME-derived default for GOCACHE.
//
// This distinction exists because a second-review pass caught a real bug:
// the sandbox image's non-root user's actual $HOME is /home/sandbox (from
// `useradd --create-home`), not /root, so Go's default
// GOCACHE=$HOME/.cache/go-build resolved to a path nothing was mounted at
// — a real `go build ./...` failed with "mkdir /home/sandbox/.cache:
// read-only file system", reproduced and confirmed in this session. Fixing
// only the mount *path* would still not be enough: Go's build cache
// (unlike its module cache) is written to on essentially every build for
// any package whose exact content hash isn't already cached, so mounting
// it read-only — which is what the original implementation did, matching
// the card's "everything else ro/absent" as an over-broad default — breaks
// real (not just cold-cache-slower) validation-command workloads. The
// fix is two-layered: (1) mount GOBUILD's cache read-write, matching what
// Go's build cache actually needs to do, and (2) set GOCACHE/GOMODCACHE
// explicitly via env rather than trusting HOME-derived defaults, so this
// stops depending on $HOME resolution at all.
type CacheKind string

const (
	// CacheKindGoMod is the downloaded-module-source cache (GOMODCACHE).
	// Mounted read-only: go only ever reads already-downloaded module
	// sources from it — fetching a genuinely new module version needs
	// network access regardless of this mount's mode, so read-write here
	// would buy nothing and only widens the write surface unnecessarily.
	CacheKindGoMod CacheKind = "gomod"
	// CacheKindGoBuild is the compiled-object build cache (GOCACHE).
	// Mounted read-write: Go writes a new cache entry for any package/test
	// binary that isn't already present under its exact content hash,
	// which is normal for real validation-command workloads (not every
	// permutation is pre-warmed) — a read-only mount here breaks `go
	// build`/`go test`, not just their speed. This does not reopen any
	// network path: writing a compiled object to a local cache directory
	// needs no egress. Go's build cache is content-addressed (keyed by a
	// hash of its inputs), so a task writing garbage into a *shared*
	// gobuild-cache volume across runs cannot make an unrelated future
	// build silently resolve to that garbage — at worst it pollutes disk
	// space under keys that never collide with legitimate future lookups,
	// the same tradeoff CI systems generally accept when caching Go build
	// output across jobs.
	CacheKindGoBuild CacheKind = "gobuild"
)

// CacheMount is one cache the sandbox needs so validation commands resolve
// packages/build artifacts locally instead of needing runtime module-proxy
// or compiler-cache-warming network access (docs/PLAN.md Task 34 Steps) —
// this narrows the sandbox's real network need to almost nothing. Source is
// whatever the underlying engine resolves it as: a named volume (the same
// gomod-cache / gobuild-cache volumes deploy/docker-compose.yaml's `dev`
// service already warms, Task 1) in the dev-container and local-socket-
// mount lanes, or a plain host directory (e.g. `go env GOMODCACHE` on the
// CI runner) in the bare-runner CI lane — both are valid `-v
// <source>:<target>` values, so this package does not need to distinguish
// them.
type CacheMount struct {
	Kind   CacheKind
	Target string
	Source string
}

// DefaultCacheMounts returns the cache mounts for the two named volumes
// Task 1's docker-compose.yaml already declares (gomod-cache,
// gobuild-cache), read from environment overrides so each lane can supply
// its own resolved source: the *_SRC env vars below, falling back to the
// bare volume name (correct for the local-socket-mount lane, where the
// engine resolves a plain name against the already-running compose
// project's volume namespace). Empty CacheMounts (i.e. neither Task 1
// volume applies, such as a lane with no dev-warmed cache at all) is left
// to the caller — this function never fabricates a source that wasn't
// configured. GoBuild's Target is under /home/sandbox — the sandbox
// image's actual non-root user home (see CacheKindGoBuild's doc comment) —
// not /root; this only stays correct as long as Config.User keeps the
// image's default uid:gid (10001:10001, matching deploy/images/
// executor.Dockerfile's `useradd --create-home` for that same uid), which
// is also this package's own default.
func DefaultCacheMounts() []CacheMount {
	return []CacheMount{
		{Kind: CacheKindGoMod, Target: "/go/pkg/mod", Source: envOr("FOUNDRY_SANDBOX_GOMOD_CACHE_SRC", "gomod-cache")},
		{Kind: CacheKindGoBuild, Target: "/home/sandbox/.cache/go-build", Source: envOr("FOUNDRY_SANDBOX_GOBUILD_CACHE_SRC", "gobuild-cache")},
	}
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// Config parameterizes one Runner. Zero-value fields are filled with
// defaults by withDefaults; WorkspaceHost and Allowlist have no safe
// default and must be set by the caller.
type Config struct {
	// Engine is the container CLI binary (default "podman").
	Engine string
	// Image is the sandbox image tag (default DefaultImage).
	Image string
	// WorkspaceHost is the host path bind-mounted read-write at
	// WorkspaceTarget. Required.
	WorkspaceHost string
	// WorkspaceTarget is the path inside the sandbox WorkspaceHost is
	// mounted at (default "/workspace").
	WorkspaceTarget string
	// CacheMounts are read-only cache mounts, e.g. DefaultCacheMounts().
	CacheMounts []CacheMount
	// Allowlist is the egress policy the gate enforces. Required.
	Allowlist EgressAllowlist
	// CPULimit is the engine's --cpus value (default "2").
	CPULimit string
	// MemoryLimit is the engine's --memory value (default "2g").
	MemoryLimit string
	// PIDsLimit is the engine's --pids-limit value (default 256).
	PIDsLimit int
	// EnvAllowlist names the only host environment variables copied into
	// the sandboxed command's environment — the same scrub discipline as
	// executor.RunSubprocess (internal/executor/subprocess.go), applied a
	// second time at the container boundary.
	EnvAllowlist []string
	// User is the engine's --user value, "<uid>:<gid>" (default
	// "10001:10001") — never root, per the governing doc's "non-root user"
	// recommended control.
	User string
	// Timeout bounds RunCommand (default 30m).
	Timeout time.Duration
	// AllowlistHostPath is the host path to the egress allowlist YAML,
	// bind-mounted read-only into the gate container. Required for Start.
	AllowlistHostPath string
	// GateExtraHosts adds "--add-host" entries to the gate container only
	// ("hostname:ip-or-host-gateway" pairs). Production allowlist entries
	// name real, publicly-resolvable hosts (e.g. api.anthropic.com) that
	// need no help from this field; it exists so tests can point a
	// deliberately-fake allowlisted hostname (never a real provider name)
	// at a local mock endpoint without touching real DNS.
	GateExtraHosts []string
	// GateAllowPrivateIPs disables the gate's private/reserved-IP
	// rejection check (gate/main.go's isPrivateOrReservedIP — defense
	// against DNS rebinding). DEV/TEST ONLY: production wiring must never
	// set this true — the one shipped allowlist entry (api.anthropic.com)
	// is a real public host and needs no exception. It exists only because
	// this package's own tests deliberately reach a mock endpoint via a
	// private/link-local docker-gateway address (GateExtraHosts above),
	// which the check would otherwise correctly reject.
	GateAllowPrivateIPs bool
}

func (c Config) withDefaults() Config {
	if c.Engine == "" {
		c.Engine = defaultEngine
	}
	if c.Image == "" {
		c.Image = DefaultImage
	}
	if c.WorkspaceTarget == "" {
		c.WorkspaceTarget = defaultWorkspaceTarget
	}
	if c.CPULimit == "" {
		c.CPULimit = defaultCPULimit
	}
	if c.MemoryLimit == "" {
		c.MemoryLimit = defaultMemoryLimit
	}
	if c.PIDsLimit == 0 {
		c.PIDsLimit = defaultPIDsLimit
	}
	if c.User == "" {
		c.User = defaultUser
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) validate() error {
	if c.WorkspaceHost == "" {
		return fmt.Errorf("sandbox: Config.WorkspaceHost is required")
	}
	if len(c.Allowlist.Allow) == 0 {
		return fmt.Errorf("sandbox: Config.Allowlist has no entries")
	}
	if err := c.Allowlist.Validate(); err != nil {
		return fmt.Errorf("sandbox: Config.Allowlist: %w", err)
	}
	if c.AllowlistHostPath == "" {
		return fmt.Errorf("sandbox: Config.AllowlistHostPath is required")
	}
	return nil
}

// Runner owns the lifecycle of one run's private network and gate sidecar.
// One Runner is one task's sandbox; RunCommand may be called more than once
// against the same Runner (e.g. Commands then ValidationCommands) but
// Start/Close bracket the whole task, not each command.
type Runner struct {
	cfg    Config
	id     string
	engine string

	started bool
	// gateAddr is the gate container's own IP address on r.networkName()
	// (resolved once, in Start, via container inspect). RunCommand's
	// sandbox container talks to the gate by this IP, not by its container
	// name — see the comment on resolveGateIPAddress for why name-based
	// resolution is not used here.
	gateAddr string
}

// NewRunner validates cfg, applies defaults, and returns a Runner. It does
// not start any container or network yet — call Start for that.
func NewRunner(cfg Config) (*Runner, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("sandbox: generate run id: %w", err)
	}
	return &Runner{cfg: cfg, id: id, engine: cfg.Engine}, nil
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *Runner) networkName() string { return "foundry-sbx-net-" + r.id }
func (r *Runner) gateName() string    { return "foundry-sbx-gate-" + r.id }
func (r *Runner) sandboxName() string { return "foundry-sbx-run-" + r.id }

// externalNetworkName returns the pre-existing engine-default network the
// gate container is additionally attached to for real internet egress.
// Overridable via FOUNDRY_SANDBOX_EXTERNAL_NETWORK for any host whose
// default network name differs from both conventions below (e.g. a
// non-default netavark/CNI configuration).
func (r *Runner) externalNetworkName() string {
	if v := os.Getenv("FOUNDRY_SANDBOX_EXTERNAL_NETWORK"); v != "" {
		return v
	}
	if r.engine == "podman" {
		return externalNetworkPodman
	}
	return externalNetwork
}

// Start creates the run's private internal network, launches the gate
// sidecar (multi-homed onto that network and the engine's external
// network), and fixes up ownership of any read-write cache mount
// (CacheKindGoBuild). Callers must call Close when the task reaches a
// terminal state.
func (r *Runner) Start(ctx context.Context) error {
	if r.started {
		return fmt.Errorf("sandbox: Runner already started")
	}
	if _, err := r.exec(ctx, buildNetworkCreateArgs(r.networkName())); err != nil {
		return fmt.Errorf("sandbox: create network: %w", err)
	}
	if _, err := r.exec(ctx, buildGateRunArgs(r.cfg, r.gateName(), r.networkName())); err != nil {
		_, _ = r.exec(ctx, buildNetworkRemoveArgs(r.networkName()))
		return fmt.Errorf("sandbox: start gate: %w", err)
	}
	// Address the gate by its own IP on this network, not by its container
	// name: an earlier version of this code relied on the network's
	// embedded DNS resolving r.gateName(), which proved unreliable in this
	// repo's own CI (confirmed live: RunCommand's real invocations failing
	// with curl's "Could not resolve proxy" — a DNS-resolution failure —
	// and, separately, a DNS-readiness probe using `getent hosts` inside a
	// container on this same network never succeeding either, even for
	// Runners that otherwise worked fine before). Whatever the underlying
	// cause, the container's IP address is assigned at creation time (not
	// racy the way name registration apparently is here) and sidesteps the
	// whole class of problem rather than trying to out-wait it.
	gateIP, err := r.resolveGateIPAddress(ctx)
	if err != nil {
		_, _ = r.exec(ctx, buildContainerRemoveArgs(r.gateName()))
		_, _ = r.exec(ctx, buildNetworkRemoveArgs(r.networkName()))
		return fmt.Errorf("sandbox: resolve gate IP address: %w", err)
	}
	r.gateAddr = gateIP
	// The gate's own listening socket may not be bound the instant `docker
	// run -d` returns (its entrypoint process has been exec'd, but the Go
	// binary itself still needs a moment to start listening). Block Start()
	// on the SAME TCP connectivity a real sandbox container will need,
	// rather than a fixed sleep (too short under worse load, or wastefully
	// long in the common case).
	if _, err := r.exec(ctx, buildGateReadinessArgs(r.cfg, r.gateAddr, r.networkName())); err != nil {
		_, _ = r.exec(ctx, buildContainerRemoveArgs(r.gateName()))
		_, _ = r.exec(ctx, buildNetworkRemoveArgs(r.networkName()))
		return fmt.Errorf("sandbox: wait for gate to accept connections: %w", err)
	}
	if _, err := r.exec(ctx, buildNetworkConnectArgs(r.externalNetworkName(), r.gateName())); err != nil {
		_, _ = r.exec(ctx, buildContainerRemoveArgs(r.gateName()))
		_, _ = r.exec(ctx, buildNetworkRemoveArgs(r.networkName()))
		return fmt.Errorf("sandbox: attach gate to external network: %w", err)
	}
	if args := buildCacheChownArgs(r.cfg); args != nil {
		if _, err := r.exec(ctx, args); err != nil {
			_, _ = r.exec(ctx, buildContainerRemoveArgs(r.gateName()))
			_, _ = r.exec(ctx, buildNetworkRemoveArgs(r.networkName()))
			return fmt.Errorf("sandbox: fix up cache mount ownership: %w", err)
		}
	}
	r.started = true
	return nil
}

// RunCommand runs argv inside a fresh sandbox container attached only to
// this Runner's private internal network, under the FS jail and resource
// caps from Config, and returns the same CommandResult shape
// executor.RunSubprocess does. Start must have been called first.
func (r *Runner) RunCommand(ctx context.Context, argv []string, timeout time.Duration) (executor.CommandResult, error) {
	if !r.started {
		return executor.CommandResult{}, fmt.Errorf("sandbox: Runner not started")
	}
	if len(argv) == 0 {
		return executor.CommandResult{}, fmt.Errorf("sandbox: empty command")
	}
	if timeout <= 0 {
		timeout = r.cfg.Timeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	envFilePath, envFileCleanup, err := writeEnvFile(r.cfg.EnvAllowlist)
	if err != nil {
		return executor.CommandResult{}, err
	}
	defer envFileCleanup()

	args := buildSandboxRunArgs(r.cfg, r.sandboxName(), r.networkName(), r.gateAddr, envFilePath, argv)
	cmdLine := strings.Join(append([]string{r.engine}, args...), " ")

	start := time.Now()
	stdout, stderr, exitCode, runErr := r.execCapture(runCtx, args)
	result := executor.CommandResult{
		Cmd:      cmdLine,
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Duration: time.Since(start),
	}
	if runCtx.Err() != nil {
		result.TimedOut = true
		_, _ = r.exec(context.Background(), buildContainerKillArgs(r.sandboxName()))
		return result, fmt.Errorf("sandbox: %q: %w", cmdLine, runCtx.Err())
	}
	if runErr != nil {
		return result, fmt.Errorf("sandbox: run %q: %w", cmdLine, runErr)
	}
	return result, nil
}

// Close tears down the gate container and the private network. Safe to
// call once per Runner; it is not idempotent against repeated calls (the
// second call will error trying to remove already-removed resources) —
// callers own calling it exactly once, same contract as
// worktree.Workspace.Release.
func (r *Runner) Close(ctx context.Context) error {
	var errs []string
	if _, err := r.exec(ctx, buildContainerRemoveArgs(r.gateName())); err != nil {
		errs = append(errs, err.Error())
	}
	if _, err := r.exec(ctx, buildNetworkRemoveArgs(r.networkName())); err != nil {
		errs = append(errs, err.Error())
	}
	r.started = false
	r.gateAddr = ""
	if len(errs) > 0 {
		return fmt.Errorf("sandbox: close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// resolveGateIPAddress returns the gate container's own IP address on
// r.networkName(), read directly from the engine rather than assumed or
// looked up by name — see Start's comment for why this Runner addresses
// the gate by IP, not by container name.
func (r *Runner) resolveGateIPAddress(ctx context.Context) (string, error) {
	format := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", r.networkName())
	out, err := r.exec(ctx, []string{"inspect", "--format", format, r.gateName()})
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(out)
	if ip == "" {
		return "", fmt.Errorf("sandbox: %s reported no IP address for %s on network %s", r.engine, r.gateName(), r.networkName())
	}
	return ip, nil
}

// exec runs an engine subcommand (network create/rm, container rm/kill) and
// discards output beyond returning it for error messages.
func (r *Runner) exec(ctx context.Context, args []string) (string, error) {
	// #nosec G204 -- args is built exclusively by this package's own
	// buildXxxArgs functions from validated Config fields, never from
	// unsanitized external input.
	cmd := exec.CommandContext(ctx, r.engine, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", r.engine, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// execCapture runs the sandbox container and returns stdout/stderr
// separately plus its exit code.
func (r *Runner) execCapture(ctx context.Context, args []string) (stdout, stderr string, exitCode int, err error) {
	// #nosec G204 -- see exec's identical justification above.
	cmd := exec.CommandContext(ctx, r.engine, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), code, nil
		}
		return outBuf.String(), errBuf.String(), code, runErr
	}
	return outBuf.String(), errBuf.String(), code, nil
}

// --- pure command-builder functions (unit-testable without any engine) ---

func buildNetworkCreateArgs(name string) []string {
	return []string{"network", "create", "--internal", name}
}

func buildNetworkRemoveArgs(name string) []string {
	return []string{"network", "rm", name}
}

func buildNetworkConnectArgs(network, container string) []string {
	return []string{"network", "connect", network, container}
}

func buildContainerRemoveArgs(name string) []string {
	return []string{"rm", "-f", name}
}

func buildContainerKillArgs(name string) []string {
	return []string{"kill", name}
}

// buildCacheChownArgs constructs a one-shot, root-privileged helper
// invocation that chowns cfg.User's uid:gid onto every CacheKindGoBuild
// mount's Target, so the non-root sandbox user can actually write new
// build-cache entries into what may be a freshly created, root-owned named
// volume (confirmed in this session: a brand-new named volume mounted
// read-write defaults to root:root 0755, which a non-root uid cannot write
// into — chown is genuinely required, not just the rw mount mode change).
// Returns nil if there is nothing to chown (no CacheKindGoBuild mount
// configured), so callers can skip running it entirely. Capabilities are
// pared back to exactly CHOWN and DAC_OVERRIDE — root with --cap-drop=ALL
// still cannot chown, or create a directory inside another uid's
// restrictive-permission directory (confirmed in this session: `mkdir -p
// /home/sandbox/.cache/...` failed with "Permission denied" under
// --cap-add=CHOWN alone, since /home/sandbox itself is 0700, owned by the
// sandbox uid, not root — DAC_OVERRIDE is what lets root traverse/write it
// regardless), without either — rather than granting this helper full root
// privilege.
func buildCacheChownArgs(cfg Config) []string {
	var script strings.Builder
	for _, m := range cfg.CacheMounts {
		if m.Kind != CacheKindGoBuild || m.Source == "" || m.Target == "" {
			continue
		}
		fmt.Fprintf(&script, "mkdir -p %s && chown -R %s %s; ", m.Target, cfg.User, m.Target)
	}
	if script.Len() == 0 {
		return nil
	}
	args := []string{
		"run", "--rm",
		"--network", "none",
		"--user", "0:0",
		"--cap-drop=ALL",
		"--cap-add=CHOWN",
		"--cap-add=DAC_OVERRIDE",
		"--security-opt", "no-new-privileges",
	}
	for _, m := range cfg.CacheMounts {
		if m.Kind != CacheKindGoBuild || m.Source == "" || m.Target == "" {
			continue
		}
		args = append(args, "-v", m.Source+":"+m.Target+":rw")
	}
	args = append(args, cfg.Image, "sh", "-c", script.String())
	return args
}

// buildGateRunArgs constructs the args that launch the egress-gate sidecar,
// detached, named, on the run's private network, with the allowlist YAML
// bind-mounted read-only and no other filesystem access, no cache mounts,
// no workspace — the gate never touches task content, only tunnels bytes.
func buildGateRunArgs(cfg Config, gateName, network string) []string {
	args := []string{
		"run", "-d",
		"--name", gateName,
		"--network", network,
		"--read-only",
		"--cap-drop=ALL",
		"--security-opt", "no-new-privileges",
		"--user", cfg.User,
		"-v", cfg.AllowlistHostPath + ":" + gateImageAllowlistPath + ":ro",
	}
	for _, h := range cfg.GateExtraHosts {
		args = append(args, "--add-host", h)
	}
	args = append(args,
		cfg.Image,
		"/usr/local/bin/foundry-egress-gate",
		"-addr", ":"+strconv.Itoa(gatePort),
		"-allowlist", gateImageAllowlistPath,
	)
	if cfg.GateAllowPrivateIPs {
		args = append(args, "-allow-private-ips")
	}
	return args
}

// gateReadinessMaxAttempts/gateReadinessInterval bound how long
// buildGateReadinessArgs' generated script will retry — 40 * 250ms = 10s,
// comfortably above any observed listener-startup delay, while still
// failing loudly (not hanging indefinitely) if the gate genuinely never
// starts accepting connections.
const (
	gateReadinessMaxAttempts = 40
	gateReadinessInterval    = "0.25"
)

// buildGateReadinessArgs constructs a short-lived probe container, attached
// to the same internal network the real sandbox container will use, that
// retries a plain TCP connection to gateIP:gatePort until it succeeds or the
// attempt budget is exhausted. Uses curl (already in cfg.Image for the
// sandbox's own task workloads, so no extra tool dependency) purely for its
// connection attempt — any response at all, even an HTTP error, proves the
// listener is up; this never inspects what curl returns beyond its exit
// code. A single container running an internal retry loop (rather than this
// package retrying many separate `docker run`s from the host) minimizes
// per-attempt container-creation overhead.
func buildGateReadinessArgs(cfg Config, gateIP, network string) []string {
	target := fmt.Sprintf("http://%s:%d/", gateIP, gatePort)
	script := fmt.Sprintf(
		"i=0; while [ \"$i\" -lt %d ]; do curl -s -o /dev/null --max-time 1 %s && exit 0; i=$((i+1)); sleep %s; done; echo \"gate at %s never accepted a connection after %d attempts\" >&2; exit 1",
		gateReadinessMaxAttempts, target, gateReadinessInterval, target, gateReadinessMaxAttempts,
	)
	return []string{
		"run", "--rm",
		"--network", network,
		"--entrypoint", "sh",
		cfg.Image,
		"-c", script,
	}
}

// buildSandboxRunArgs constructs the args that launch one task command
// inside the FS-jailed, capped, non-root sandbox container, attached only
// to the run's private network (no route out except to the gate).
// envFilePath, if non-empty, is passed as --env-file: cfg.EnvAllowlist's
// values are written there (writeEnvFile) rather than embedded literally as
// "-e VAR=value" argv tokens, since argv (unlike a subprocess's envp, and
// unlike --env-file) is visible to any host user who can list processes
// (`ps aux`/`/proc/<pid>/cmdline`) — internal/executor/subprocess.go
// already avoids this class of leak by passing environment via envp, not
// argv; this mirrors that discipline at the container boundary. Only
// EnvAllowlist values go through the env-file: HTTPS_PROXY/HTTP_PROXY/
// NO_PROXY name only this run's own internal gate container (by IP, not
// name — see Start's comment on resolveGateIPAddress), never a secret, so
// they stay as plain -e args.
func buildSandboxRunArgs(cfg Config, name, network, gateAddr, envFilePath string, argv []string) []string {
	args := []string{
		"run", "--rm",
		"--name", name,
		"--network", network,
		"--read-only",
		"--cap-drop=ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", strconv.Itoa(cfg.PIDsLimit),
		"--memory", cfg.MemoryLimit,
		"--cpus", cfg.CPULimit,
		"--user", cfg.User,
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"-v", cfg.WorkspaceHost + ":" + cfg.WorkspaceTarget + ":rw",
		"-w", cfg.WorkspaceTarget,
	}
	for _, m := range cfg.CacheMounts {
		if m.Source == "" || m.Target == "" {
			continue
		}
		mode := "ro"
		if m.Kind == CacheKindGoBuild {
			mode = "rw"
		}
		args = append(args, "-v", m.Source+":"+m.Target+":"+mode)
		switch m.Kind {
		case CacheKindGoMod:
			args = append(args, "-e", "GOMODCACHE="+m.Target)
		case CacheKindGoBuild:
			args = append(args, "-e", "GOCACHE="+m.Target)
		}
	}
	if envFilePath != "" {
		args = append(args, "--env-file", envFilePath)
	}
	gateURL := "http://" + gateAddr + ":" + strconv.Itoa(gatePort)
	args = append(args,
		"-e", "HTTPS_PROXY="+gateURL,
		"-e", "HTTP_PROXY="+gateURL,
		"-e", "NO_PROXY=",
	)
	args = append(args, cfg.Image)
	args = append(args, argv...)
	return args
}

// writeEnvFile writes allowlist's currently-set values to a fresh, 0600,
// process-private temp file in docker/podman --env-file format
// ("KEY=VALUE" per line) and returns its path plus a cleanup func the
// caller must invoke once the container has been launched — the file lives
// outside the sandboxed workspace (so the sandboxed process can never read
// its own env-file back off disk) for exactly as long as the container
// launch needs it. Returns an empty path and a no-op cleanup if allowlist
// is empty, so callers can omit --env-file entirely rather than pass one
// pointing at an empty file.
func writeEnvFile(allowlist []string) (path string, cleanup func(), err error) {
	noop := func() {}
	if len(allowlist) == 0 {
		return "", noop, nil
	}

	f, err := os.CreateTemp("", "foundry-sandbox-env-*")
	if err != nil {
		return "", noop, fmt.Errorf("sandbox: create env file: %w", err)
	}
	cleanup = func() { _ = os.Remove(f.Name()) }

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, fmt.Errorf("sandbox: chmod env file: %w", err)
	}

	var b strings.Builder
	for _, name := range allowlist {
		v, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		// docker/podman's --env-file format is one "KEY=VALUE" per line
		// with no quoting/escaping; a literal newline in v would silently
		// corrupt the file into extra bogus entries, so reject rather than
		// risk that.
		if strings.ContainsAny(v, "\n\r") {
			_ = f.Close()
			cleanup()
			return "", noop, fmt.Errorf("sandbox: env var %q contains a newline, incompatible with --env-file", name)
		}
		fmt.Fprintf(&b, "%s=%s\n", name, v)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, fmt.Errorf("sandbox: write env file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("sandbox: close env file: %w", err)
	}
	return f.Name(), cleanup, nil
}
