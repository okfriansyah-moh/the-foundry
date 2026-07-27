package secrets

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Store.Get when scope/name has no stored
// secret. Implementations must wrap this sentinel (fmt.Errorf("...: %w",
// ErrNotFound)) rather than returning an unrelated error, so callers can
// distinguish "no such secret" from a backend failure via errors.Is.
var ErrNotFound = errors.New("secrets: not found")

// Store is the seam every secret read in Foundry goes through. scope is a
// profile ID (see doc.go); name is the secret's logical name within that
// scope (e.g. "github_token", "telegram_bot_token", "anthropic_api_key").
//
// Get must never log, wrap into another package's error without care, or
// otherwise surface the returned value anywhere but its own return —
// callers are equally bound by that once they have the string in hand.
type Store interface {
	Get(ctx context.Context, scope, name string) (string, error)
}
