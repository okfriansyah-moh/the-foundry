package sandbox

import (
	"os"
	"strings"
	"testing"
)

func testAllowlist() EgressAllowlist {
	return EgressAllowlist{Version: 1, Allow: []EgressRule{{Host: "api.anthropic.com", Port: 443}}}
}

func TestConfig_WithDefaults(t *testing.T) {
	cfg := Config{WorkspaceHost: "/tmp/ws", Allowlist: testAllowlist(), AllowlistHostPath: "/tmp/allow.yaml"}.withDefaults()

	if cfg.Engine != "podman" {
		t.Errorf("Engine default = %q, want podman", cfg.Engine)
	}
	if cfg.Image != DefaultImage {
		t.Errorf("Image default = %q, want %q", cfg.Image, DefaultImage)
	}
	if cfg.WorkspaceTarget != "/workspace" {
		t.Errorf("WorkspaceTarget default = %q", cfg.WorkspaceTarget)
	}
	if cfg.CPULimit != "2" || cfg.MemoryLimit != "2g" || cfg.PIDsLimit != 256 {
		t.Errorf("resource defaults wrong: cpu=%q mem=%q pids=%d", cfg.CPULimit, cfg.MemoryLimit, cfg.PIDsLimit)
	}
	if cfg.User != "10001:10001" {
		t.Errorf("User default = %q", cfg.User)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout default = %v", cfg.Timeout)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid",
			cfg:     Config{WorkspaceHost: "/tmp/ws", Allowlist: testAllowlist(), AllowlistHostPath: "/tmp/allow.yaml"}.withDefaults(),
			wantErr: false,
		},
		{
			name:    "missing workspace host",
			cfg:     Config{Allowlist: testAllowlist(), AllowlistHostPath: "/tmp/allow.yaml"}.withDefaults(),
			wantErr: true,
		},
		{
			name:    "empty allowlist",
			cfg:     Config{WorkspaceHost: "/tmp/ws", AllowlistHostPath: "/tmp/allow.yaml"}.withDefaults(),
			wantErr: true,
		},
		{
			name:    "missing allowlist host path",
			cfg:     Config{WorkspaceHost: "/tmp/ws", Allowlist: testAllowlist()}.withDefaults(),
			wantErr: true,
		},
		{
			name: "wildcard allowlist rejected even if non-empty",
			cfg: Config{
				WorkspaceHost:     "/tmp/ws",
				AllowlistHostPath: "/tmp/allow.yaml",
				Allowlist:         EgressAllowlist{Version: 1, Allow: []EgressRule{{Host: "*", Port: 443}}},
			}.withDefaults(),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewRunner_RejectsInvalidConfig(t *testing.T) {
	if _, err := NewRunner(Config{}); err == nil {
		t.Fatalf("expected NewRunner to reject an empty Config")
	}
}

func TestNewRunner_AssignsUniqueIDs(t *testing.T) {
	cfg := Config{WorkspaceHost: "/tmp/ws", Allowlist: testAllowlist(), AllowlistHostPath: "/tmp/allow.yaml"}
	r1, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r2, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if r1.id == r2.id {
		t.Fatalf("expected distinct run ids, got %q twice", r1.id)
	}
	if r1.networkName() == r2.networkName() || r1.gateName() == r2.gateName() || r1.sandboxName() == r2.sandboxName() {
		t.Fatalf("expected per-run resource names to be namespaced by id")
	}
}

func TestBuildNetworkCreateArgs_IsInternal(t *testing.T) {
	args := buildNetworkCreateArgs("foundry-sbx-net-abc")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--internal") {
		t.Fatalf("expected --internal in network create args, got %q", joined)
	}
	if !strings.Contains(joined, "foundry-sbx-net-abc") {
		t.Fatalf("expected network name in args, got %q", joined)
	}
}

func TestBuildGateRunArgs_NoWorkspaceOrCacheMounts(t *testing.T) {
	cfg := Config{
		Image:             DefaultImage,
		User:              "10001:10001",
		AllowlistHostPath: "/host/config/sandbox-egress-allowlist.yaml",
	}
	args := buildGateRunArgs(cfg, "foundry-sbx-gate-abc", "foundry-sbx-net-abc")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--read-only", "--cap-drop=ALL", "no-new-privileges",
		"--user 10001:10001",
		"/host/config/sandbox-egress-allowlist.yaml:" + gateImageAllowlistPath + ":ro",
		"foundry-egress-gate",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("gate run args missing %q; got %q", want, joined)
		}
	}
	if strings.Contains(joined, "/workspace") {
		t.Errorf("gate must not mount the task workspace; got %q", joined)
	}
}

func TestBuildGateReadinessArgs_RetriesResolutionOnSameNetwork(t *testing.T) {
	cfg := Config{Image: DefaultImage}
	args := buildGateReadinessArgs(cfg, "foundry-sbx-gate-abc", "foundry-sbx-net-abc")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--network foundry-sbx-net-abc",
		"getent hosts foundry-sbx-gate-abc",
		DefaultImage,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("gate readiness args missing %q; got %q", want, joined)
		}
	}
	if strings.Contains(joined, "--rm --rm") {
		t.Errorf("unexpected duplicated --rm flag; got %q", joined)
	}
}

func TestBuildSandboxRunArgs_FSJailAndCaps(t *testing.T) {
	cfg := Config{
		Image:           DefaultImage,
		WorkspaceHost:   "/host/ws",
		WorkspaceTarget: "/workspace",
		CacheMounts: []CacheMount{
			{Kind: CacheKindGoMod, Target: "/go/pkg/mod", Source: "gomod-cache"},
			{Kind: CacheKindGoBuild, Target: "/home/sandbox/.cache/go-build", Source: "gobuild-cache"},
		},
		CPULimit:    "2",
		MemoryLimit: "2g",
		PIDsLimit:   256,
		User:        "10001:10001",
	}
	args := buildSandboxRunArgs(cfg, "foundry-sbx-run-abc", "foundry-sbx-net-abc", "foundry-sbx-gate-abc", "", []string{"go", "build", "./..."})
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--read-only", "--cap-drop=ALL", "no-new-privileges",
		"--pids-limit 256", "--memory 2g", "--cpus 2",
		"--user 10001:10001",
		"/host/ws:/workspace:rw",
		// GOMODCACHE mount stays read-only (only ever read once a module
		// is downloaded); explicit GOMODCACHE= env var, not relying on
		// GOPATH-derived defaults.
		"gomod-cache:/go/pkg/mod:ro",
		"GOMODCACHE=/go/pkg/mod",
		// GOBUILD cache mount is read-write (Go writes new cache entries
		// on essentially every build) and its target is under the image's
		// actual non-root $HOME, not /root — the exact bug a second-review
		// pass caught; explicit GOCACHE= env var, not relying on
		// $HOME-derived defaults.
		"gobuild-cache:/home/sandbox/.cache/go-build:rw",
		"GOCACHE=/home/sandbox/.cache/go-build",
		"HTTPS_PROXY=http://foundry-sbx-gate-abc:8080",
		"HTTP_PROXY=http://foundry-sbx-gate-abc:8080",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("sandbox run args missing %q; got %q", want, joined)
		}
	}
	if strings.Contains(joined, "/go/pkg/mod:rw") {
		t.Errorf("GOMODCACHE must never be mounted read-write, got %q", joined)
	}
	// The trailing argv (the actual task command) must appear last, verbatim.
	if args[len(args)-3] != "go" || args[len(args)-2] != "build" || args[len(args)-1] != "./..." {
		t.Errorf("expected argv to be appended verbatim at the end, got tail %v", args[len(args)-3:])
	}
}

func TestBuildSandboxRunArgs_SkipsCacheMountsWithEmptySource(t *testing.T) {
	cfg := Config{
		Image:           DefaultImage,
		WorkspaceHost:   "/host/ws",
		WorkspaceTarget: "/workspace",
		CacheMounts:     []CacheMount{{Kind: CacheKindGoMod, Target: "/go/pkg/mod", Source: ""}},
		User:            "10001:10001",
	}
	args := buildSandboxRunArgs(cfg, "n", "net", "gate", "", []string{"true"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "/go/pkg/mod") {
		t.Errorf("expected empty-source cache mount to be skipped, got %q", joined)
	}
}

// TestBuildSandboxRunArgs_EnvAllowlistGoesThroughEnvFileNotArgv is the fix
// for a second-review finding: EnvAllowlist values used to be embedded
// literally as "-e VAR=value" argv tokens, visible to any host user who
// can list processes (`ps aux`/`/proc/<pid>/cmdline`) — unlike
// internal/executor/subprocess.go, which passes environment via envp, not
// argv. Values must now flow only through --env-file (a private temp
// file), never appear in argv at all.
func TestBuildSandboxRunArgs_EnvAllowlistGoesThroughEnvFileNotArgv(t *testing.T) {
	cfg := Config{
		Image:           DefaultImage,
		WorkspaceHost:   "/host/ws",
		WorkspaceTarget: "/workspace",
		EnvAllowlist:    []string{"FOUNDRY_SANDBOX_TEST_SECRET"},
		User:            "10001:10001",
	}
	args := buildSandboxRunArgs(cfg, "n", "net", "gate", "/tmp/fake-env-file", []string{"true"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--env-file /tmp/fake-env-file") {
		t.Errorf("expected --env-file to be passed through, got %q", joined)
	}
	if strings.Contains(joined, "FOUNDRY_SANDBOX_TEST_SECRET") {
		t.Errorf("EnvAllowlist names/values must never appear directly in argv, got %q", joined)
	}
}

func TestBuildSandboxRunArgs_OmitsEnvFileFlagWhenPathEmpty(t *testing.T) {
	cfg := Config{Image: DefaultImage, WorkspaceHost: "/host/ws", WorkspaceTarget: "/workspace", User: "10001:10001"}
	args := buildSandboxRunArgs(cfg, "n", "net", "gate", "", []string{"true"})
	if strings.Contains(strings.Join(args, " "), "--env-file") {
		t.Errorf("expected no --env-file flag when envFilePath is empty, got %v", args)
	}
}

func TestWriteEnvFile_WritesOnlySetAllowlistedVars(t *testing.T) {
	t.Setenv("FOUNDRY_SANDBOX_TEST_ALLOWED", "visible-value")
	// FOUNDRY_SANDBOX_TEST_UNSET is deliberately never set.

	path, cleanup, err := writeEnvFile([]string{"FOUNDRY_SANDBOX_TEST_ALLOWED", "FOUNDRY_SANDBOX_TEST_UNSET"})
	if err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}
	defer cleanup()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected env file perm 0600, got %o", perm)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if string(content) != "FOUNDRY_SANDBOX_TEST_ALLOWED=visible-value\n" {
		t.Errorf("unexpected env file content: %q", content)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected cleanup to remove the env file, stat err = %v", err)
	}
}

func TestWriteEnvFile_EmptyAllowlistReturnsNoPath(t *testing.T) {
	path, cleanup, err := writeEnvFile(nil)
	if err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}
	defer cleanup()
	if path != "" {
		t.Errorf("expected empty path for an empty allowlist, got %q", path)
	}
}

func TestWriteEnvFile_RejectsNewlineInValue(t *testing.T) {
	t.Setenv("FOUNDRY_SANDBOX_TEST_MULTILINE", "line1\nFAKE_VAR=injected")
	_, cleanup, err := writeEnvFile([]string{"FOUNDRY_SANDBOX_TEST_MULTILINE"})
	defer cleanup()
	if err == nil {
		t.Fatalf("expected a newline in an env value to be rejected")
	}
}

func TestBuildCacheChownArgs_OnlyTargetsGoBuildMount(t *testing.T) {
	cfg := Config{
		Image: DefaultImage,
		User:  "10001:10001",
		CacheMounts: []CacheMount{
			{Kind: CacheKindGoMod, Target: "/go/pkg/mod", Source: "gomod-cache"},
			{Kind: CacheKindGoBuild, Target: "/home/sandbox/.cache/go-build", Source: "gobuild-cache"},
		},
	}
	args := buildCacheChownArgs(cfg)
	if args == nil {
		t.Fatalf("expected non-nil args when a CacheKindGoBuild mount is configured")
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--user 0:0", "--cap-drop=ALL", "--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE", "no-new-privileges",
		"gobuild-cache:/home/sandbox/.cache/go-build:rw",
		"chown -R 10001:10001 /home/sandbox/.cache/go-build",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("cache-chown args missing %q; got %q", want, joined)
		}
	}
	if strings.Contains(joined, "gomod-cache") {
		t.Errorf("expected the read-only gomod-cache mount to be excluded from chown, got %q", joined)
	}
}

func TestBuildCacheChownArgs_NilWhenNoWritableCacheMount(t *testing.T) {
	cfg := Config{
		Image:       DefaultImage,
		User:        "10001:10001",
		CacheMounts: []CacheMount{{Kind: CacheKindGoMod, Target: "/go/pkg/mod", Source: "gomod-cache"}},
	}
	if args := buildCacheChownArgs(cfg); args != nil {
		t.Errorf("expected nil args when no CacheKindGoBuild mount is configured, got %v", args)
	}
}

// TestRunner_ExternalNetworkName_PerEngineDefault is the regression test for
// a genuine compatibility gap docs/PLAN.md Task 97 caught: Docker's default
// bridge network is literally named "bridge"; Podman's default network
// (netavark or CNI) is named "podman". Runner.Start attaches the gate
// container to this network for real internet egress — a hardcoded
// "bridge" would fail outright ("network not found") the first time this
// package's Runner.Start ever actually ran against podman, which no session
// prior to Task 97 had exercised (Task 34 validated Config.Engine="podman"'s
// default only against a docker engine).
func TestRunner_ExternalNetworkName_PerEngineDefault(t *testing.T) {
	cfg := Config{WorkspaceHost: "/tmp/ws", Allowlist: testAllowlist(), AllowlistHostPath: "/tmp/allow.yaml"}

	dockerCfg := cfg
	dockerCfg.Engine = "docker"
	r, err := NewRunner(dockerCfg)
	if err != nil {
		t.Fatalf("NewRunner (docker): %v", err)
	}
	if got := r.externalNetworkName(); got != "bridge" {
		t.Errorf("docker externalNetworkName() = %q, want %q", got, "bridge")
	}

	podmanCfg := cfg
	podmanCfg.Engine = "podman"
	r, err = NewRunner(podmanCfg)
	if err != nil {
		t.Fatalf("NewRunner (podman): %v", err)
	}
	if got := r.externalNetworkName(); got != "podman" {
		t.Errorf("podman externalNetworkName() = %q, want %q", got, "podman")
	}
}

func TestRunner_ExternalNetworkName_EnvOverrideWins(t *testing.T) {
	t.Setenv("FOUNDRY_SANDBOX_EXTERNAL_NETWORK", "my-custom-net")
	cfg := Config{
		Engine:            "podman",
		WorkspaceHost:     "/tmp/ws",
		Allowlist:         testAllowlist(),
		AllowlistHostPath: "/tmp/allow.yaml",
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if got := r.externalNetworkName(); got != "my-custom-net" {
		t.Errorf("externalNetworkName() = %q, want env override %q", got, "my-custom-net")
	}
}

func TestDefaultCacheMounts_UsesEnvOverride(t *testing.T) {
	t.Setenv("FOUNDRY_SANDBOX_GOMOD_CACHE_SRC", "/runner/go/pkg/mod")
	t.Setenv("FOUNDRY_SANDBOX_GOBUILD_CACHE_SRC", "")

	mounts := DefaultCacheMounts()
	if len(mounts) != 2 {
		t.Fatalf("expected 2 default cache mounts, got %d", len(mounts))
	}
	if mounts[0].Kind != CacheKindGoMod || mounts[0].Source != "/runner/go/pkg/mod" {
		t.Errorf("expected env override to apply, got %+v", mounts[0])
	}
	if mounts[1].Kind != CacheKindGoBuild || mounts[1].Source != "gobuild-cache" {
		t.Errorf("expected fallback to bare volume name, got %+v", mounts[1])
	}
	if mounts[1].Target != "/home/sandbox/.cache/go-build" {
		t.Errorf("expected gobuild cache target under the sandbox user's real $HOME, got %q", mounts[1].Target)
	}
}
