// Package local is the local-model executor adapter (docs/PLAN.md Task 79 /
// EVO-06): any OpenAI-compatible endpoint (e.g. Ollama) via the shared
// apiexec helper. Cost is zero (with optional shadow accounting). Because it
// runs locally, it may be granted customer data classes that hosted
// providers are not. Adapter selection is the kernel's job (Task 85).
package local
