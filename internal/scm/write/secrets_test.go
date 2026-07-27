package write_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
	"github.com/okfriansyah-moh/the-foundry/internal/secrets"
	"github.com/okfriansyah-moh/the-foundry/internal/secrets/filestore"
)

// newTestStore constructs a real age-encrypted filestore.Store rooted at
// t.TempDir(), mirroring Task 35's own test discipline
// (internal/secrets/filestore/filestore_test.go's newTestStore) rather
// than a hand-rolled fake secrets.Store — SecretsTokenSource's contract is
// with the real backend, not an interface mock.
func newTestStore(t *testing.T) (*filestore.Store, *bytes.Buffer) {
	t.Helper()
	t.Setenv("FILESTORE_TEST_PASSPHRASE", "correct-horse-battery-staple")
	dir := t.TempDir()
	var logBuf bytes.Buffer
	s := filestore.New(filepath.Join(dir, "secrets.age"), filestore.PassphraseKeySource{EnvVar: "FILESTORE_TEST_PASSPHRASE"})
	s.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	return s, &logBuf
}

func TestSecretsTokenSource_RoundTripsThroughRealFilestore(t *testing.T) {
	store, logBuf := newTestStore(t)
	ctx := context.Background()

	const tokenValue = "ghp_test-token-value-should-never-be-logged"
	if err := store.Set(ctx, "profile-a", write.DefaultTokenSecretName, tokenValue); err != nil {
		t.Fatalf("Set: %v", err)
	}
	logBuf.Reset()

	src := write.SecretsTokenSource{Store: store, Scope: "profile-a"}
	got, err := src.Token(ctx)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != tokenValue {
		t.Fatalf("Token() = %q, want %q", got, tokenValue)
	}

	logged := logBuf.String()
	if strings.Contains(logged, tokenValue) {
		t.Fatalf("audit log leaked the token value: %s", logged)
	}
}

func TestSecretsTokenSource_DefaultsNameWhenEmpty(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	const tokenValue = "ghp_default-name-token"
	if err := store.Set(ctx, "profile-b", "github_token", tokenValue); err != nil {
		t.Fatalf("Set: %v", err)
	}

	src := write.SecretsTokenSource{Store: store, Scope: "profile-b"} // Name left empty
	got, err := src.Token(ctx)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != tokenValue {
		t.Fatalf("Token() = %q, want %q", got, tokenValue)
	}
}

func TestSecretsTokenSource_MissingSecretErrorsWithoutLeakingValue(t *testing.T) {
	store, logBuf := newTestStore(t)
	ctx := context.Background()

	src := write.SecretsTokenSource{Store: store, Scope: "profile-missing"}
	got, err := src.Token(ctx)
	if err == nil {
		t.Fatalf("Token: expected error for missing secret, got value %q", got)
	}
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Token error = %v, want wrapping secrets.ErrNotFound", err)
	}
	if got != "" {
		t.Fatalf("Token() on error = %q, want empty string", got)
	}

	// The error string itself must never contain a secret value — there is
	// none provisioned here, but scope/name (not a token value) are the
	// only caller-controlled strings allowed to appear.
	if strings.Contains(err.Error(), "profile-missing") == false {
		t.Fatalf("error should include scope for debuggability: %v", err)
	}
	_ = logBuf // audit log presence already covered by filestore's own tests
}

func TestSecretsTokenSource_CustomNameOverridesDefault(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	const tokenValue = "ghp_custom-name-token"
	if err := store.Set(ctx, "profile-c", "custom_github_token", tokenValue); err != nil {
		t.Fatalf("Set: %v", err)
	}

	src := write.SecretsTokenSource{Store: store, Scope: "profile-c", Name: "custom_github_token"}
	got, err := src.Token(ctx)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != tokenValue {
		t.Fatalf("Token() = %q, want %q", got, tokenValue)
	}

	// The default name must not have been used.
	other := write.SecretsTokenSource{Store: store, Scope: "profile-c"}
	if _, err := other.Token(ctx); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("expected default-name lookup to miss, got err=%v", err)
	}
}

var _ write.TokenSource = write.SecretsTokenSource{}
var _ write.TokenSource = write.EnvTokenSource{}
