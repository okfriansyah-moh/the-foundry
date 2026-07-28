// Package apicontracttest is the shared contract suite for API-class executor
// adapters (docs/PLAN.md Task 79 / EVO-06), the API-provider analog of
// internal/executor/contracttest. It runs each adapter against an httptest
// server standing in for the OpenAI-compatible endpoint, so unit tests need
// no real API key or network. Gated live tests (RUN_REAL_EXECUTOR=1) live in
// each adapter package.
package apicontracttest
