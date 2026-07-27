// Package filestore is the file-backed implementation of
// internal/secrets.Store (docs/PLAN.md Task 35 / FND-16, Blocker B4):
// secrets live in a single age-encrypted file, DefaultPath()
// ("~/.foundry/secrets.age" expanded via os.UserHomeDir), as a
// scope -> name -> value JSON document before encryption.
//
// Key management (the card's "key from OS keychain or passphrase env for
// CI") is pluggable via the KeySource interface:
//
//   - PassphraseKeySource derives an age scrypt identity/recipient pair
//     from an environment variable (FOUNDRY_SECRETS_PASSPHRASE by
//     default) — the CI path, no OS keychain required.
//   - KeychainKeySource stores a generated age X25519 identity in the OS
//     keychain (via github.com/zalando/go-keyring) — the local-developer
//     path.
//
// DefaultKeySource picks passphrase-env when set, keychain otherwise.
//
// Every Get call is audited via slog (scope, name, found/not-found —
// never the value) before returning, satisfying the card's "audit read
// events" requirement.
package filestore
