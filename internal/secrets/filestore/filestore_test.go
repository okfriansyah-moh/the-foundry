package filestore_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/secrets"
	"github.com/okfriansyah-moh/the-foundry/internal/secrets/filestore"
)

func newTestStore(t *testing.T) (*filestore.Store, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	s := filestore.New(filepath.Join(dir, "secrets.age"), filestore.PassphraseKeySource{EnvVar: "FILESTORE_TEST_PASSPHRASE"})
	s.Logger = logger
	return s, &logBuf
}

func TestStore_GetOnEmptyStoreReturnsNotFound(t *testing.T) {
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	s, _ := newTestStore(t)

	_, err := s.Get(context.Background(), "profile-a", "github_token")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get on empty store: got %v, want ErrNotFound", err)
	}
}

func TestStore_SetThenGetRoundTrips(t *testing.T) {
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	s, _ := newTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "profile-a", "github_token", "gh-secret-value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get(ctx, "profile-a", "github_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "gh-secret-value" {
		t.Fatalf("Get: got %q, want %q", got, "gh-secret-value")
	}
}

func TestStore_ScopeIsolation(t *testing.T) {
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	s, _ := newTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "profile-a", "github_token", "value-a"); err != nil {
		t.Fatalf("Set profile-a: %v", err)
	}
	if err := s.Set(ctx, "profile-b", "github_token", "value-b"); err != nil {
		t.Fatalf("Set profile-b: %v", err)
	}

	a, err := s.Get(ctx, "profile-a", "github_token")
	if err != nil || a != "value-a" {
		t.Fatalf("Get profile-a: got (%q, %v), want value-a", a, err)
	}
	b, err := s.Get(ctx, "profile-b", "github_token")
	if err != nil || b != "value-b" {
		t.Fatalf("Get profile-b: got (%q, %v), want value-b", b, err)
	}
}

func TestStore_SetOverwrites(t *testing.T) {
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	s, _ := newTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "profile-a", "telegram_bot_token", "first"); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := s.Set(ctx, "profile-a", "telegram_bot_token", "second"); err != nil {
		t.Fatalf("Set second: %v", err)
	}
	got, err := s.Get(ctx, "profile-a", "telegram_bot_token")
	if err != nil || got != "second" {
		t.Fatalf("Get: got (%q, %v), want second", got, err)
	}
}

// TestStore_FileIsEncryptedAtRest proves the on-disk file never contains
// the plaintext secret — the file's own bytes, read directly (bypassing
// the Store entirely), must not contain the value Set wrote.
func TestStore_FileIsEncryptedAtRest(t *testing.T) {
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	s, _ := newTestStore(t)
	ctx := context.Background()

	const secretValue = "super-duper-plaintext-github-token-value"
	if err := s.Set(ctx, "profile-a", "github_token", secretValue); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatalf("read %s: %v", s.Path, err)
	}
	if bytes.Contains(raw, []byte(secretValue)) {
		t.Fatalf("secrets.age contains the plaintext secret value — encryption did not happen")
	}
}

// TestStore_WrongPassphraseFailsToDecrypt proves the passphrase actually
// gates access — a Store pointed at the same file with a different
// passphrase must not be able to read it back.
func TestStore_WrongPassphraseFailsToDecrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.age")

	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	writer := filestore.New(path, filestore.PassphraseKeySource{EnvVar: "FILESTORE_TEST_PASSPHRASE"})
	if err := writer.Set(context.Background(), "profile-a", "github_token", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	t.Setenv("FILESTORE_TEST_PASSPHRASE", "wrong-passphrase")
	reader := filestore.New(path, filestore.PassphraseKeySource{EnvVar: "FILESTORE_TEST_PASSPHRASE"})
	if _, err := reader.Get(context.Background(), "profile-a", "github_token"); err == nil {
		t.Fatalf("Get with wrong passphrase: got nil error, want a decryption failure")
	}
}

// TestStore_AuditLogsReadsWithoutLeakingValue proves the card's "audit
// read events" requirement two ways at once: an entry is emitted per Get
// call, and the emitted entry never contains the secret's own value.
func TestStore_AuditLogsReadsWithoutLeakingValue(t *testing.T) {
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	s, logBuf := newTestStore(t)
	ctx := context.Background()

	const secretValue = "audit-test-should-never-appear-in-logs"
	if err := s.Set(ctx, "profile-a", "github_token", secretValue); err != nil {
		t.Fatalf("Set: %v", err)
	}
	logBuf.Reset() // Set's own load/save don't audit; only Get does — but reset defensively

	if _, err := s.Get(ctx, "profile-a", "github_token"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "scope=profile-a") || !strings.Contains(logged, "name=github_token") {
		t.Fatalf("audit log missing expected fields: %s", logged)
	}
	if !strings.Contains(logged, "found=true") {
		t.Fatalf("audit log missing found=true: %s", logged)
	}
	if strings.Contains(logged, secretValue) {
		t.Fatalf("audit log leaked the secret value: %s", logged)
	}
}

func TestPassphraseKeySource_MissingEnvVarErrors(t *testing.T) {
	os.Unsetenv("FILESTORE_TEST_MISSING_PASSPHRASE")
	src := filestore.PassphraseKeySource{EnvVar: "FILESTORE_TEST_MISSING_PASSPHRASE"}

	if _, err := src.Identity(context.Background()); err == nil {
		t.Fatal("Identity: got nil error with no passphrase set, want error")
	}
	if _, err := src.Recipient(context.Background()); err == nil {
		t.Fatal("Recipient: got nil error with no passphrase set, want error")
	}
}

func TestDefaultPath_EndsInFoundrySecretsAge(t *testing.T) {
	path, err := filestore.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(path), ".foundry/secrets.age") {
		t.Fatalf("DefaultPath: got %q, want suffix .foundry/secrets.age", path)
	}
}
