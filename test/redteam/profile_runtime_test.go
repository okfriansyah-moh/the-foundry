package redteam

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

func TestProfileRuntime_CrossDomainDenied(t *testing.T) {
	r := profile.RuntimeIsolation{ProfileID: "personal-1", Kind: profile.Personal}
	if err := r.CompatibleWithEnvelope("org-profile", "org-1"); err == nil {
		t.Fatal("expected denial")
	}
}
