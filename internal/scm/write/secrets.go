package write

import (
	"context"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/secrets"
)

// TokenSource is the secrets seam Pusher depends on for SCM authentication.
// Task 35 (FND-16) provides internal/secrets.Store; EnvTokenSource is the
// documented CI/local fallback, and SecretsTokenSource reads profile-scoped
// tokens from the store (Task 137). Depending on this interface rather than
// calling os.Getenv directly from Pusher means swapping implementations
// touches only the wiring that constructs a Pusher, never PushBranch logic.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// DefaultTokenEnvVar is the environment variable EnvTokenSource reads when
// its own EnvVar field is left empty (GitHub path).
const DefaultTokenEnvVar = "GITHUB_TOKEN"

// BitbucketTokenEnvVar is the environment variable name for a Bitbucket
// Cloud API token / app password used by BitbucketPusher over HTTP
// (docs/PLAN.md Task 137). Wire EnvTokenSource{EnvVar: BitbucketTokenEnvVar}
// — never reuse DefaultTokenEnvVar for Bitbucket.
const BitbucketTokenEnvVar = "BITBUCKET_API_TOKEN"

// EnvTokenSource reads a GitHub personal access token from an environment
// variable.
//
// Least-privilege scope (document this wherever the token is provisioned):
// this package performs exactly one write operation — pushing a branch —
// and nothing else (no PR creation, no repo administration). The token
// this env var holds must therefore be scoped to:
//   - a classic PAT: the "repo" scope only (GitHub does not offer a
//     narrower classic scope that still permits pushing to private
//     repositories); or, preferably,
//   - a fine-grained PAT: "Contents: Read and write" on the specific
//     target repository only — no other repository permission, no
//     account permission, and access restricted to the one organization/
//     repository this token is provisioned for.
//
// Never grant this token pull-request, issues, administration, or
// workflow scopes — this package has no code path that would use them,
// and Task 27's own Boundary forbids adding one.
type EnvTokenSource struct {
	// EnvVar overrides the environment variable name. Empty means
	// DefaultTokenEnvVar.
	EnvVar string
}

// Token implements TokenSource by reading EnvVar (or DefaultTokenEnvVar).
// It never logs the value it reads.
func (s EnvTokenSource) Token(_ context.Context) (string, error) {
	name := s.EnvVar
	if name == "" {
		name = DefaultTokenEnvVar
	}
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("scm/write: environment variable %s is not set (needs a least-privilege GitHub PAT — see EnvTokenSource doc comment)", name)
	}
	return v, nil
}

// DefaultTokenSecretName is the secret name SecretsTokenSource reads when
// its own Name field is left empty (GitHub path).
const DefaultTokenSecretName = "github_token"

// DefaultBitbucketTokenSecretName is the secrets-store name for a Bitbucket
// Cloud token (docs/PLAN.md Task 137). Wire
// SecretsTokenSource{Name: DefaultBitbucketTokenSecretName}.
const DefaultBitbucketTokenSecretName = "bitbucket_token"

// SecretsTokenSource reads a GitHub token from internal/secrets.Store —
// the production default once Task 137 wires Pusher.Tokens (EnvTokenSource
// above stays the explicit fallback/CI path). Scope is the profile ID the
// token belongs to (internal/secrets's scope model is profile-bound: see
// internal/secrets's doc.go); Name defaults to DefaultTokenSecretName when
// empty.
//
// Same least-privilege scope requirement as EnvTokenSource's doc comment
// (fine-grained PAT, "Contents: Read and write" on the one target repo
// only, no PR/issues/administration/workflow scopes) — Store.Get returns
// whichever token value was provisioned under scope/name, so the scope
// discipline lives at provisioning time, not here.
type SecretsTokenSource struct {
	// Store is the secrets seam this type reads from.
	Store secrets.Store
	// Scope is the profile ID the token is scoped to.
	Scope string
	// Name overrides the secret name. Empty means DefaultTokenSecretName.
	Name string
}

// Token implements TokenSource by reading Name (or DefaultTokenSecretName)
// from Store under Scope. It never logs the value it reads; Store.Get's
// own contract (internal/secrets's doc.go) forbids logging or otherwise
// surfacing it too.
func (s SecretsTokenSource) Token(ctx context.Context) (string, error) {
	name := s.Name
	if name == "" {
		name = DefaultTokenSecretName
	}
	v, err := s.Store.Get(ctx, s.Scope, name)
	if err != nil {
		return "", fmt.Errorf("scm/write: read GitHub token from secrets store (scope %q, name %q): %w", s.Scope, name, err)
	}
	return v, nil
}
