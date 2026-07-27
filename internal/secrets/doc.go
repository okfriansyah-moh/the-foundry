// Package secrets defines the one seam every secret read in Foundry goes
// through (docs/PLAN.md Task 35 / FND-16, Blocker B4): Store.Get(ctx, scope,
// name). Today the only implementation is internal/secrets/filestore, an
// age-encrypted file backend; a Vault/KMS-backed implementation can be added
// later behind this same interface without any consuming package changing
// (B4's own default: "age-file behind interface (Task 35); Vault/KMS at
// M2+").
//
// Scope model: scope is profile-bound — callers pass the profile ID
// (internal/profile.Profile.ID) the secret belongs to, so the same secret
// name ("github_token", "telegram_bot_token", ...) can hold a different
// value per profile. This package does not import internal/profile itself
// (scope is a plain string here, not a profile.Profile) to avoid a
// dependency a secrets seam has no need for; callers own the mapping from
// their own profile identifier to this string.
//
// This package never logs, wraps, or otherwise surfaces a secret's
// plaintext value in an error or log message — every error path below
// (and every implementation of Store) must uphold that, since it is the
// property Task 35's leak scanner (cmd/fitlint's secretsleak check) exists
// to catch a regression of.
package secrets
