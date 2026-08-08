package openai

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
const Name = "openai"

const (
	defaultBaseURL = "https://api.openai.com/v1"
	baseURLEnv     = "FOUNDRY_OPENAI_BASE_URL" // test seam / override
	defaultModel   = "gpt-5.4"
	modelEnv       = "FOUNDRY_OPENAI_MODEL" // blunt per-provider override (per-class model lives in apiexec.ModelPolicy)
	apiKeyEnv      = "OPENAI_API_KEY"
	pricingVersion = "openai-2026-07"
	modelPolicyEnv = "FOUNDRY_EXECUTOR_MODELS" // path to config/executor-models.yaml
)

// New constructs a fresh OpenAI adapter. It is granted only non-customer data
// by default; customer data requires an explicit grant added here. The
// config-driven per-task-class model policy is loaded from
// config/executor-models.yaml (FOUNDRY_EXECUTOR_MODELS overrides the path). A
// MISSING file is non-fatal (per-class routing disabled); a present-but-
// invalid file panics at construction — a deploy-time misconfiguration should
// fail closed, mirroring executor.Register's wiring-bug panic convention.
// Precedence: a per-class ModelPolicy hit overrides the blunt ModelEnv
// (FOUNDRY_OPENAI_MODEL) override when both are set.
func New() *apiexec.Adapter {
	policy, err := apiexec.LoadModelPolicyWithRuntimeFallback(modelPolicyPath())
	if err != nil {
		panic(fmt.Sprintf("openai: invalid model policy: %v", err))
	}
	return apiexec.New(apiexec.Config{
		Provider:           Name,
		BaseURL:            defaultBaseURL,
		BaseURLEnv:         baseURLEnv,
		Model:              defaultModel,
		ModelEnv:           modelEnv,
		ModelPolicy:        policy,
		APIKeyEnv:          apiKeyEnv,
		PricingVersion:     pricingVersion,
		CostPerCallUSD:     0.01,
		GrantedDataClasses: nil, // no customer-data grant by default
		DefaultTimeout:     5 * time.Minute,
	})
}

func modelPolicyPath() string {
	if p := os.Getenv(modelPolicyEnv); p != "" {
		return p
	}
	return "config/executor-models.yaml"
}
