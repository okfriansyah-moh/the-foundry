package opencode

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/contracttest"
)

func TestContract(t *testing.T) {
	contracttest.Run(t, contracttest.Options{
		Name:           Name,
		New:            func() executor.Adapter { return New() },
		BinEnvOverride: binaryEnvOverride,
		AuthEnvVar:     authEnvVar,
	})
}

func TestContractLeak(t *testing.T) {
	contracttest.LeakCheck(t, contracttest.Options{
		Name: Name,
		New:  func() executor.Adapter { return New() },
	})
}
