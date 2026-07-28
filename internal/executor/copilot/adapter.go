package copilot

import (
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/cliexec"
)

func init() {
	executor.Register(Name, func() executor.Adapter { return New() })
}

// Name is the executor registry name for this adapter.
const Name = "copilot"

const (
	defaultBinary     = "copilot"
	binaryEnvOverride = "FOUNDRY_COPILOT_BIN" // test seam only
	promptFileName    = ".foundry-copilot-prompt.md"
	authEnvVar        = "GH_TOKEN"
	defaultTimeout    = 30 * time.Minute
)

// headlessArgs runs the Copilot CLI non-interactively (programmatic mode),
// reading the prompt from stdin. See docs/notes/copilot-cli-flags.md (dated
// snapshot; re-verify per Task 17's staleness rule).
var headlessArgs = []string{"-p", "--allow-all-tools"}

// allowedEnv is the EXHAUSTIVE environment allowlist visible to the Copilot
// subprocess. It never honors TaskPacket.EnvAllowlist. Copilot authenticates
// via GitHub credentials, so GH_TOKEN/GITHUB_TOKEN are the passed secrets.
var allowedEnv = []string{
	"PATH",         // resolve tools the agent invokes
	"HOME",         // locate Copilot config/credentials
	"GH_TOKEN",     // GitHub CLI auth
	"GITHUB_TOKEN", // alternate GitHub auth var
}

// New constructs a fresh Copilot adapter.
func New() *cliexec.Adapter {
	return cliexec.New(cliexec.Config{
		Provider:       Name,
		Binary:         defaultBinary,
		BinEnvOverride: binaryEnvOverride,
		Args:           headlessArgs,
		AllowedEnv:     allowedEnv,
		PromptFile:     promptFileName,
		DefaultTimeout: defaultTimeout,
	})
}
