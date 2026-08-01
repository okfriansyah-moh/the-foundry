package write

import "github.com/go-git/go-git/v5/plumbing/transport/http"

// AuthForTest exposes authFor for package-external tests (Task 137).
func AuthForTest(provider Provider, remote, token string) *http.BasicAuth {
	return authFor(provider, remote, token)
}
