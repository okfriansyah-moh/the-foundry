package local

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/apicontracttest"
)

func TestContract(t *testing.T) {
	apicontracttest.Run(t, apicontracttest.Options{
		Name:       Name,
		New:        func() executor.Adapter { return New() },
		BaseURLEnv: baseURLEnv,
	})
}
