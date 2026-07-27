package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/secrets/filestore"
)

// TestRun_SecretsStoreInjectsConfiguredEnvVar proves the Task 35 migration:
// when Secrets is configured, the value it returns ends up in the
// subprocess env under SecretsEnvVar, overriding whatever the ambient
// process env already had there.
func TestRun_SecretsStoreInjectsConfiguredEnvVar(t *testing.T) {
	t.Setenv("CLAUDECODE_SECRETS_TEST_PASSPHRASE", "correct-horse-battery-staple")
	store := filestore.New(filepath.Join(t.TempDir(), "secrets.age"), filestore.PassphraseKeySource{EnvVar: "CLAUDECODE_SECRETS_TEST_PASSPHRASE"})
	ctx := context.Background()
	if err := store.Set(ctx, "profile-a", "anthropic_api_key", "sk-from-secrets-store"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "env_dump.txt")
	stub := writeStub(t, dir, "claude", `env > `+dumpPath+`
cat <<'EOF'
{"result":"ok","session_id":"s1","num_turns":1,"duration_ms":10,"total_cost_usd":0.01,"usage":{"input_tokens":1}}
EOF
`)
	t.Setenv(binaryEnvOverride, stub)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ambient-should-be-overridden")

	ws := newWorkspace(t)
	a := New()
	a.Secrets = store
	a.SecretsScope = "profile-a"

	if err := a.Prepare(ctx, ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := a.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	dump, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	got := string(dump)
	if !strings.Contains(got, "ANTHROPIC_API_KEY=sk-from-secrets-store") {
		t.Fatalf("subprocess env missing secrets-store-sourced key, dump:\n%s", got)
	}
	if strings.Contains(got, "sk-ambient-should-be-overridden") {
		t.Fatalf("ambient env value leaked instead of being overridden, dump:\n%s", got)
	}

	// The process-wide env var must be restored to its pre-Run value once
	// Run returns, so a second Adapter without Secrets configured sees the
	// original ambient value again, not this Run's injected one.
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "sk-ambient-should-be-overridden" {
		t.Fatalf("ANTHROPIC_API_KEY not restored after Run: got %q", v)
	}
}

// TestRun_SecretsStoreMissingSecretErrors proves a misconfigured Secrets
// seam fails loudly rather than silently falling back to the ambient env.
func TestRun_SecretsStoreMissingSecretErrors(t *testing.T) {
	t.Setenv("CLAUDECODE_SECRETS_TEST_PASSPHRASE_2", "correct-horse-battery-staple")
	store := filestore.New(filepath.Join(t.TempDir(), "secrets.age"), filestore.PassphraseKeySource{EnvVar: "CLAUDECODE_SECRETS_TEST_PASSPHRASE_2"})

	dir := t.TempDir()
	stub := writeStub(t, dir, "claude", `echo '{"result":"ok"}'`)
	t.Setenv(binaryEnvOverride, stub)

	ws := newWorkspace(t)
	a := New()
	a.Secrets = store
	a.SecretsScope = "profile-never-provisioned"

	ctx := context.Background()
	if err := a.Prepare(ctx, ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := a.Run(ctx); err == nil {
		t.Fatal("Run: got nil error for a secret never provisioned, want error")
	}
}
