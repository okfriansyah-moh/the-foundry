package cursor

import (
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/cliexec"
)

func init() {
	executor.Register(Name, func() executor.Adapter { return New() })
}

// Name is the executor registry name for this adapter.
const Name = "cursor"

const (
	defaultBinary     = "cursor-agent"
	binaryEnvOverride = "FOUNDRY_CURSOR_BIN" // test seam only
	promptFileName    = ".foundry-cursor-prompt.md"
	authEnvVar        = "CURSOR_API_KEY"
	defaultTimeout    = 30 * time.Minute
)

// headlessArgs runs the Cursor CLI non-interactively, reading the prompt from
// stdin. See docs/notes/cursor-cli-flags.md (dated snapshot; re-verify per
// Task 17's staleness rule).
var headlessArgs = []string{"--print"}

// allowedEnv is the EXHAUSTIVE environment allowlist visible to the Cursor
// subprocess. It never honors TaskPacket.EnvAllowlist.
var allowedEnv = []string{
	"PATH",           // resolve tools the agent invokes
	"HOME",           // locate Cursor config/credentials
	"CURSOR_API_KEY", // provider auth
}

// New constructs a fresh Cursor adapter.
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
