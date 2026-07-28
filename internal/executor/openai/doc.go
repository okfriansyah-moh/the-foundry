// Package openai is the OpenAI API-class executor adapter (docs/PLAN.md Task
// 79 / EVO-06). It speaks the OpenAI /chat/completions protocol via the
// shared apiexec helper. No capability is assumed — features are declared in
// config/executor-capabilities.yaml, and customer data is never sent without
// an explicit grant (apiexec.GuardDataClass). Adapter selection is the
// kernel's job (Task 85), never this package's.
package openai
