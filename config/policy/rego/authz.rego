# Runtime authorization rules for internal/policy/pdp (Task 23, FND-04).
#
# Input shape (see internal/policy.Request / internal/policy/pdp.buildInput):
#   {
#     "principal":     "<string>",
#     "action":        "permission" | "notify" | "execute" | "deploy",
#     "resource":      "<string>",
#     "context":       {<action-specific keys, e.g. "env", "approved">},
#     "policy_digest": "<sha256:... — the compiler.Resolved digest the caller believes is current>",
#     "policy":        {<compiler.Policy, JSON-tagged fields>}
#   }
#
# This module ONLY evaluates the already-merged `input.policy` (Task 22's
# compiler.Resolved.Effective) — it never merges layers and never sees raw
# platform/org/profile/workflow inputs. It is the runtime half of the
# compiler-vs-PDP split (docs/foundry/docs/security/authorization-model.md).
package foundry.authz

default allow = false

default reason = "denied: no matching policy rule for this action"

allow {
	input.action == "permission"
	input.resource == input.policy.permissions_allowlist[_]
}

allow {
	input.action == "notify"
	input.resource == input.policy.notification_classes[_]
}

allow {
	input.action == "execute"
	input.resource == input.policy.executor_allowlist[_]
}

allow {
	input.action == "deploy"
	input.policy.deployment_modes[input.context.env] == "auto"
}

allow {
	input.action == "deploy"
	input.policy.deployment_modes[input.context.env] == "command"
	input.context.approved == true
}

reason = "allowed" {
	allow
}

reason = r {
	not allow
	input.action == "permission"
	r := sprintf("denied: permission %q not in permissions_allowlist", [input.resource])
}

reason = r {
	not allow
	input.action == "notify"
	r := sprintf("denied: notification class %q not in notification_classes", [input.resource])
}

reason = r {
	not allow
	input.action == "execute"
	r := sprintf("denied: executor %q not in executor_allowlist", [input.resource])
}

reason = r {
	not allow
	input.action == "deploy"
	r := sprintf("denied: deployment to env %q not permitted (mode/approval)", [input.context.env])
}

# ---------------------------------------------------------------------------
# internal/api route authorization (docs/PLAN.md Task 36, FND-17).
#
# Every internal/api route calls through this same Decider with
# action == "api" and resource == "api:<verb>:<noun>" (see
# internal/api/server.go's registerRoutes). No governing doc defines a
# per-route RBAC taxonomy for this API yet (authorization-model.md's
# conformance tests, and this file's four rules above, cover plan
# execution's permission/notify/execute/deploy decisions, not general API
# CRUD) — per docs/PLAN.md's no-gaps rule, this is the smallest reversible
# rule: any principal who already holds a valid session JWT (enforced
# upstream by internal/api's own bearer-token check before this Decide
# call ever runs) is authorized. This still exercises the real, pinned,
# tamper-evident OPA bundle for every route; it does not yet encode
# fine-grained per-route/per-principal policy, which is future scope once
# a governing doc specifies one.
allow {
	input.action == "api"
	input.principal != ""
}

reason = r {
	not allow
	input.action == "api"
	r := "denied: no authenticated principal"
}
