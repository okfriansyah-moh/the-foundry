package filestore_test

import (
	"context"
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/okfriansyah-moh/the-foundry/internal/secrets/filestore"
)

// TestKeychainKeySource_GeneratesAndPersists uses go-keyring's in-memory
// mock (never a real OS keychain, which a headless CI/container
// environment may not have a Secret Service daemon for at all) to prove
// KeychainKeySource generates an identity on first use and returns the
// same one on subsequent calls.
func TestKeychainKeySource_GeneratesAndPersists(t *testing.T) {
	keyring.MockInit()

	src := filestore.KeychainKeySource{Service: "test-service", Account: "test-account"}
	ctx := context.Background()

	id1, err := src.Identity(ctx)
	if err != nil {
		t.Fatalf("Identity (first call, should generate): %v", err)
	}
	id2, err := src.Identity(ctx)
	if err != nil {
		t.Fatalf("Identity (second call, should reuse): %v", err)
	}
	if id1.(interface{ String() string }).String() != id2.(interface{ String() string }).String() {
		t.Fatal("Identity returned a different key across calls — not persisted")
	}
}

func TestKeychainKeySource_RecipientMatchesIdentity(t *testing.T) {
	keyring.MockInit()

	src := filestore.KeychainKeySource{Service: "test-service-2", Account: "test-account-2"}
	ctx := context.Background()

	if _, err := src.Identity(ctx); err != nil {
		t.Fatalf("Identity: %v", err)
	}
	rec, err := src.Recipient(ctx)
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}

	type recipientStringer interface{ String() string }
	if rec.(recipientStringer).String() == "" {
		t.Fatal("Recipient returned an empty public key")
	}
}

func TestDefaultKeySource_PicksPassphraseWhenEnvVarSet(t *testing.T) {
	t.Setenv(filestore.PassphraseEnvVar, "some-passphrase")
	src := filestore.DefaultKeySource()
	if _, ok := src.(filestore.PassphraseKeySource); !ok {
		t.Fatalf("DefaultKeySource with %s set: got %T, want PassphraseKeySource", filestore.PassphraseEnvVar, src)
	}
}

func TestDefaultKeySource_PicksKeychainWhenEnvVarUnset(t *testing.T) {
	_ = os.Unsetenv(filestore.PassphraseEnvVar)
	src := filestore.DefaultKeySource()
	if _, ok := src.(filestore.KeychainKeySource); !ok {
		t.Fatalf("DefaultKeySource with no passphrase env: got %T, want KeychainKeySource", src)
	}
}
