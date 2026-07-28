//go:build redteam

package redteam

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/sandbox"
)

func TestSandboxEscape_WildcardHostRejected(t *testing.T) {
	policy := sandbox.EgressAllowlist{Version: 1, Allow: []sandbox.EgressRule{{Host: "*.example.com", Port: 443, Reason: "bad"}}}
	if err := policy.Validate(); err == nil {
		t.Fatal("expected wildcard host to be rejected")
	}
}
