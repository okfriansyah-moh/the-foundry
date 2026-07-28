package geminicli

import (
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/cliexec"
)

func init() {
	executor.Register(Name, func() executor.Adapter { return New() })
}

// Name is the executor registry name for this adapter.
const Name = "gemini-cli"

const (
	defaultBinary     = "gemini"
	binaryEnvOverride = "FOUNDRY_GEMINI_CLI_BIN" // test seam only
	promptFileName    = ".foundry-gemini-cli-prompt.md"
	authEnvVar        = "GEMINI_API_KEY"
	defaultTimeout    = 30 * time.Minute
)

// headlessArgs runs Gemini CLI non-interactively, reading the prompt from
// stdin. Flags are per the installed CLI at implementation time — see
// docs/notes/gemini-cli-flags.md (dated snapshot; re-verify per Task 17's
// staleness rule).
var headlessArgs = []string{"--prompt-interactive=false"}

// allowedEnv is the EXHAUSTIVE environment allowlist visible to the `gemini`
// subprocess. It never honors TaskPacket.EnvAllowlist.
var allowedEnv = []string{
	"PATH",           // resolve tools the agent invokes
	"HOME",           // locate Gemini CLI config/credentials
	"GEMINI_API_KEY", // provider auth (API-key path)
	"GOOGLE_API_KEY", // alternate provider auth var
	"GOOGLE_GENAI_USE_VERTEXAI",
}

// New constructs a fresh Gemini CLI adapter.
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
