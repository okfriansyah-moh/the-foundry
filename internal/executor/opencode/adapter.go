package opencode

import (
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/cliexec"
)

func init() {
	executor.Register(Name, func() executor.Adapter { return New() })
}

// Name is the executor registry name for this adapter.
const Name = "opencode"

const (
	defaultBinary     = "opencode"
	binaryEnvOverride = "FOUNDRY_OPENCODE_BIN" // test seam only
	// promptFileName is fixed and never derived from packet fields, closing
	// off path traversal via packet content by construction.
	promptFileName = ".foundry-opencode-prompt.md"
	// authEnvVar is OpenCode's own auth credential var — the one secret this
	// adapter deliberately passes through.
	authEnvVar     = "OPENCODE_API_KEY"
	defaultTimeout = 30 * time.Minute
)

// headlessArgs runs OpenCode non-interactively, reading the task prompt from
// stdin. Flags are per the installed CLI at implementation time — see
// docs/notes/opencode-flags.md (dated snapshot; re-verify per the same
// staleness rule as Task 17).
var headlessArgs = []string{"run", "--print"}

// allowedEnv is the EXHAUSTIVE environment allowlist visible to the
// `opencode` subprocess. It deliberately does not honor
// TaskPacket.EnvAllowlist — the no-secret-leak property must hold regardless
// of caller input, so it is enforced here as a fixed set.
var allowedEnv = []string{
	"PATH",             // resolve tools the agent invokes
	"HOME",             // locate OpenCode's config/credentials
	"OPENCODE_CONFIG",  // optional config path override
	"OPENCODE_API_KEY", // provider auth
}

// New constructs a fresh OpenCode adapter.
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
