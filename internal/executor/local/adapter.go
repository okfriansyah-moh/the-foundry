package local

import (
	"fmt"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/apiexec"
)

func init() {
	executor.Register(Name, func() executor.Adapter { return New() })
}

// Name is the executor registry name for this adapter.
const Name = "local"

const (
	defaultBaseURL = "http://localhost:11434/v1" // Ollama's OpenAI-compatible endpoint
	baseURLEnv     = "FOUNDRY_LOCAL_BASE_URL"    // test seam / real endpoint
	defaultModel   = "llama3.1"
	modelEnv       = "FOUNDRY_LOCAL_MODEL" // blunt per-provider override (per-class model lives in apiexec.ModelPolicy)
	pricingVersion = "local-zero"
	modelPolicyEnv = "FOUNDRY_EXECUTOR_MODELS" // path to config/executor-models.yaml
)

// New constructs a fresh local-model adapter. Cost is zero; because inference
// stays on the local host, it is granted the customer data class. The
// per-task-class model policy is loaded from config/executor-models.yaml
// (FOUNDRY_EXECUTOR_MODELS overrides the path). A MISSING file is non-fatal;
// a present-but-invalid file panics at construction (fail-closed on
// misconfiguration). A per-class ModelPolicy hit overrides ModelEnv.
func New() *apiexec.Adapter {
	policy, err := apiexec.LoadModelPolicy(modelPolicyPath())
	if err != nil {
		panic(fmt.Sprintf("local: invalid model policy: %v", err))
	}
	return apiexec.New(apiexec.Config{
		Provider:           Name,
		BaseURL:            defaultBaseURL,
		BaseURLEnv:         baseURLEnv,
		Model:              defaultModel,
		ModelEnv:           modelEnv,
		ModelPolicy:        policy,
		APIKeyEnv:          "", // local endpoints typically need no auth
		PricingVersion:     pricingVersion,
		CostPerCallUSD:     0, // local = zero cost
		GrantedDataClasses: []string{"customer"},
		DefaultTimeout:     5 * time.Minute,
	})
}

func modelPolicyPath() string {
	if p := os.Getenv(modelPolicyEnv); p != "" {
		return p
	}
	return "config/executor-models.yaml"
}
