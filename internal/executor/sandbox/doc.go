// Package sandbox implements Task 34 (FND-15): the rootless OCI executor
// sandbox. It gives every executor-run command a filesystem jail (workspace
// read-write, module/build caches read-only, everything else absent), a
// default-deny network with a narrow, explicit, egress allowlist
// (config/sandbox-egress-allowlist.yaml), and cgroup resource caps
// (cpu/memory/pids) — the enforcement boundary named in
// docs/foundry/docs/security/authorization-model.md §13.4 and the OWASP-LLM
// LLM06 (excessive agency) containment point called out in
// .ai/skills/ai-vulnerability-defense/SKILL.md.
//
// # Network model
//
// The sandbox container joins a private, per-run container network created
// with the engine's "internal" flag: a network with no route to the outside
// world at all. A second container — the "gate" (this package's gate/
// subpackage) — is multi-homed onto both that internal network and the
// engine's normal external network, and runs a minimal allowlist-checked
// HTTP CONNECT relay. The sandboxed container is pointed at the gate via
// HTTPS_PROXY/HTTP_PROXY; because the *network itself* has no route out
// except to the gate, a process inside the sandbox that ignores those env
// vars and dials a disallowed host directly still fails at the network
// layer — the allowlist is enforced by topology, not by trusting the
// sandboxed process to cooperate.
//
// # Engine
//
// Config.Engine names the container CLI binary to shell out to. The task
// card specifies rootless podman (or runc) as the production engine; this
// package only assumes a docker-CLI-compatible command surface (run,
// network create/connect/rm, rm), so any engine exposing that surface
// (podman, docker) works unchanged. See oci.go's package comment for how
// this session's own validation substituted docker (podman/runc were not
// installed in this execution environment).
//
// # Authority boundary
//
// This package performs no side effects outside the ephemeral containers
// and networks it creates and tears down itself; it never touches SCM,
// deploys, or billing (Constitution C4 — those remain kernel-owned and
// happen outside the sandbox entirely, which is exactly why the egress
// allowlist below never needs to include GitHub, Stripe, or Fly).
//
// Exec role: infra+security-review (docs/PLAN.md Task 34 / FND-15).
package sandbox
