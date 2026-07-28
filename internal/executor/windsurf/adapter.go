package windsurf

import (
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/cliexec"
)

func init() {
	executor.Register(Name, func() executor.Adapter { return New() })
}

// Name is the executor registry name for this adapter.
const Name = "windsurf"

const (
	defaultBinary     = "windsurf"
	binaryEnvOverride = "FOUNDRY_WINDSURF_BIN" // test seam only
	promptFileName    = ".foundry-windsurf-prompt.md"
	authEnvVar        = "WINDSURF_API_KEY"
	defaultTimeout    = 30 * time.Minute
)

// headlessArgs runs Windsurf non-interactively, reading the prompt from
// stdin. See docs/notes/windsurf-cli-flags.md (dated snapshot; re-verify per
// Task 17's staleness rule).
var headlessArgs = []string{"--print"}

// allowedEnv is the EXHAUSTIVE environment allowlist visible to the Windsurf
// subprocess. It never honors TaskPacket.EnvAllowlist.
var allowedEnv = []string{
	"PATH",             // resolve tools the agent invokes
	"HOME",             // locate Windsurf config/credentials
	"WINDSURF_API_KEY", // provider auth
}

// New constructs a fresh Windsurf adapter.
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
