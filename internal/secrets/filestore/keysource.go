package filestore

import (
	"context"
	"errors"
	"fmt"
	"os"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

// KeySource resolves the age identity/recipient pair Store uses to
// decrypt/encrypt secrets.age. Defined here (the consuming package) per
// this repo's interfaces-in-consuming-package convention, since Store is
// the only consumer.
type KeySource interface {
	// Identity returns the age.Identity used to decrypt secrets.age.
	Identity(ctx context.Context) (age.Identity, error)
	// Recipient returns the age.Recipient used to encrypt secrets.age.
	Recipient(ctx context.Context) (age.Recipient, error)
}

// PassphraseEnvVar is the environment variable PassphraseKeySource reads
// when its own EnvVar field is empty. This is the "passphrase env for CI"
// half of the task card: CI sets this once, no OS keychain involved.
const PassphraseEnvVar = "FOUNDRY_SECRETS_PASSPHRASE"

// PassphraseKeySource derives a symmetric age scrypt identity/recipient
// pair from a passphrase read from an environment variable. Both sides
// derive from the same passphrase, so no separate key storage is needed —
// the intended fit for CI, where the passphrase itself is already a
// managed CI secret.
type PassphraseKeySource struct {
	// EnvVar overrides the environment variable name. Empty means
	// PassphraseEnvVar.
	EnvVar string
}

func (p PassphraseKeySource) envVar() string {
	if p.EnvVar != "" {
		return p.EnvVar
	}
	return PassphraseEnvVar
}

func (p PassphraseKeySource) passphrase() (string, error) {
	v := os.Getenv(p.envVar())
	if v == "" {
		return "", fmt.Errorf("filestore: environment variable %s is not set", p.envVar())
	}
	return v, nil
}

// Identity implements KeySource.
func (p PassphraseKeySource) Identity(_ context.Context) (age.Identity, error) {
	pass, err := p.passphrase()
	if err != nil {
		return nil, err
	}
	id, err := age.NewScryptIdentity(pass)
	if err != nil {
		return nil, fmt.Errorf("filestore: derive scrypt identity: %w", err)
	}
	return id, nil
}

// Recipient implements KeySource.
func (p PassphraseKeySource) Recipient(_ context.Context) (age.Recipient, error) {
	pass, err := p.passphrase()
	if err != nil {
		return nil, err
	}
	r, err := age.NewScryptRecipient(pass)
	if err != nil {
		return nil, fmt.Errorf("filestore: derive scrypt recipient: %w", err)
	}
	return r, nil
}

// KeychainService and KeychainAccount are the default OS-keychain
// coordinates KeychainKeySource stores the generated age identity under.
const (
	KeychainService = "delivery-foundry-secrets"
	KeychainAccount = "age-identity"
)

// KeychainKeySource stores a generated age X25519 identity in the OS
// keychain (macOS Keychain, Windows Credential Manager, or a Secret
// Service on Linux, via github.com/zalando/go-keyring), generating and
// persisting one on first use. This is the local-developer path — a
// headless CI/container environment without a keychain daemon should use
// PassphraseKeySource instead (see DefaultKeySource).
type KeychainKeySource struct {
	// Service and Account override the keychain coordinates. Empty means
	// KeychainService/KeychainAccount.
	Service string
	Account string
}

func (k KeychainKeySource) service() string {
	if k.Service != "" {
		return k.Service
	}
	return KeychainService
}

func (k KeychainKeySource) account() string {
	if k.Account != "" {
		return k.Account
	}
	return KeychainAccount
}

// identity loads the stored identity, generating and persisting a fresh
// one on first use (keyring.ErrNotFound).
func (k KeychainKeySource) identity() (*age.X25519Identity, error) {
	service, account := k.service(), k.account()

	v, err := keyring.Get(service, account)
	if err != nil {
		if !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("filestore: read OS keychain: %w", err)
		}
		id, genErr := age.GenerateX25519Identity()
		if genErr != nil {
			return nil, fmt.Errorf("filestore: generate age identity: %w", genErr)
		}
		if setErr := keyring.Set(service, account, id.String()); setErr != nil {
			return nil, fmt.Errorf("filestore: persist age identity to OS keychain: %w", setErr)
		}
		return id, nil
	}

	id, err := age.ParseX25519Identity(v)
	if err != nil {
		return nil, fmt.Errorf("filestore: parse identity stored in OS keychain: %w", err)
	}
	return id, nil
}

// Identity implements KeySource.
func (k KeychainKeySource) Identity(_ context.Context) (age.Identity, error) {
	return k.identity()
}

// Recipient implements KeySource.
func (k KeychainKeySource) Recipient(_ context.Context) (age.Recipient, error) {
	id, err := k.identity()
	if err != nil {
		return nil, err
	}
	return id.Recipient(), nil
}

// DefaultKeySource returns PassphraseKeySource when PassphraseEnvVar is
// set (the CI path) and KeychainKeySource otherwise (local development,
// OS keychain).
func DefaultKeySource() KeySource {
	if os.Getenv(PassphraseEnvVar) != "" {
		return PassphraseKeySource{}
	}
	return KeychainKeySource{}
}
