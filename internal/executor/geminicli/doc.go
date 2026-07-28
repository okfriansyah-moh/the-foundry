// Package geminicli is the Gemini CLI executor adapter (docs/PLAN.md Task 87
// / PRV-04). It provides provider breadth beyond the Anthropic/OpenAI
// families and is GSD-named.
//
// It mirrors Task 17's proven claude-code shape via the shared cliexec
// helper: the `gemini` CLI is run headlessly inside the workspace jail with
// the task prompt fed on stdin (never argv), under a fixed package-confined
// environment allowlist that never trusts TaskPacket.EnvAllowlist. Gemini's
// server-side caching / tool-search capabilities map onto the existing
// capability vocabulary (§6.7), not a parallel one. Adapter selection is the
// kernel's job (Task 85), never this package's.
package geminicli
