// Package apiexec is the shared implementation of an API-class executor
// adapter that speaks the OpenAI-compatible /chat/completions protocol. It
// backs both the OpenAI adapter (docs/PLAN.md Task 79 / EVO-06) and any
// OpenAI-compatible local endpoint (e.g. Ollama), which differ only by base
// URL, model, auth env var, and pricing.
//
// Like cliexec for CLI providers, apiexec holds no provider knowledge of its
// own: each concrete provider package supplies a Config. The adapter writes
// the request prompt and the response body into the workspace as evidence
// artifacts (provenance), meters cost with a pricing_version per call, and
// never leaks provider-specific fields into the untrusted Summary.
//
// Two classification/authority properties this package enforces:
//   - GuardDataClass: customer-classified data is never sent to a provider
//     that has not been granted that data class (Task 79 acceptance).
//   - Summary is untrusted telemetry only — Task 13's verifier decides done.
package apiexec
