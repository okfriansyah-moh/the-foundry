// Package claudecode materializes validated Foundry packages into Claude Code's
// workspace-local agent and skill directories. Its per-kind manifests grant no
// overwrite or deletion authority: reinstall treats exact expected bytes as a
// no-op and never replaces an existing destination, so catalog or enablement
// changes require a fresh workspace. The adapter has no executor-selection or
// side-effect authority.
package claudecode
