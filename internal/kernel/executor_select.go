package kernel

import (
	"context"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// Selection reason codes. These are machine-stable, distinguishable strings
// (docs/PLAN.md Task 89 requires kimi/kilo to fail with a NAMED, distinct
// error, not merely non-nil) carried on SelectionError alongside a reused
// verify.Classification.
const (
	// ReasonNotInAllowlist: the chosen executor is not in the policy's
	// executor_allowlist (Task 22's tighten-only field).
	ReasonNotInAllowlist = "not-in-allowlist"
	// ReasonUnknownExecutor: the chosen executor has no capability-registry
	// record at all.
	ReasonUnknownExecutor = "unknown-executor"
	// ReasonUnsupportedExecutor: the chosen executor has a registry record
	// but availability != supported (e.g. Kimi/Kilo stubs, Task 89).
	ReasonUnsupportedExecutor = "unsupported-executor"
	// ReasonNoExecutorConfigured: no explicit executor was named, no
	// routing resolved one, and no default is configured.
	ReasonNoExecutorConfigured = "no-executor-configured"
	// ReasonUnroutedClass: routing is active and the task declares a class,
	// but that class has no entry in the routing table (docs/PLAN.md Task 90
	// — an unroutable class fails closed, it never silently defaults).
	ReasonUnroutedClass = "unrouted-class"
	// ReasonNoEligibleExecutor: routing is active and the task's class is
	// routed, but none of its preferred executors are both allowlisted and a
	// supported registry record (Task 90 — fails closed, never defaults).
	ReasonNoEligibleExecutor = "no-eligible-executor"
)

// SelectionError is the fail-closed outcome of ExecutorSelector.Select. It
// is always classified verify.ClassificationPolicyViolation — selecting an
// executor the policy forbids, or that isn't a real supported provider, is
// a policy decision the kernel refuses, never a retryable infra fault.
type SelectionError struct {
	// Executor is the name that was rejected (may be empty for
	// ReasonNoExecutorConfigured).
	Executor string
	// Reason is one of the ReasonX codes above.
	Reason string
	// Classification is the verify vocabulary term callers surface.
	Classification verify.Classification
}

func (e *SelectionError) Error() string {
	if e.Executor == "" {
		return fmt.Sprintf("executor selection failed: %s", e.Reason)
	}
	return fmt.Sprintf("executor selection failed for %q: %s", e.Executor, e.Reason)
}

// ExecutorSelector is the kernel-owned, 100% deterministic decision point
// for which executor adapter runs a task (docs/PLAN.md Task 85 / PRV-02,
// Constitution C4 — the kernel decides, never PEC, never an LLM, never an
// unchecked env var). No LLM output, no PEC proposal, and no inner-loop
// phase hint (Task 92) may ever change the name Select returns: it is a
// pure function of the plan task, the resolved policy, and the capability
// registry.
type ExecutorSelector struct {
	// Default is the fallback executor name used when a task names none
	// explicitly and routing (Task 90) resolves none. Empty means "no
	// default" — an unrouted task with no explicit executor then fails
	// closed rather than silently defaulting.
	Default string

	// Routing, when non-empty, is the task-class → ordered-preference table
	// consulted for a task that names no executor explicitly (docs/PLAN.md
	// Task 90 / PRV-07). Empty (the Task 85 baseline) means "no routing" —
	// selection falls straight through to Default.
	Routing RoutingTable

	// Profile is the policy profile ID used to gate capability eligibility
	// during routing (Task 90). Empty is fine when Routing is empty.
	Profile string
}

// Select returns the executor name that must run task, or a *SelectionError
// (fail closed). Determinism is total: identical inputs always yield an
// identical result. The order is:
//
//  1. If task names an executor explicitly, that name is validated against
//     the allowlist and registry and returned (or rejected). An explicit
//     name is never overridden by routing or the default.
//  2. Otherwise, if a routing table is configured, the first task-class
//     preference that is both allowlisted and a supported registry record
//     wins (Task 90).
//  3. Otherwise the configured Default is validated and returned.
//  4. If none of the above yields a name, selection fails closed.
func (s ExecutorSelector) Select(_ context.Context, task plan.Task, pol compiler.Resolved, reg capability.Registry) (string, error) {
	allow := pol.Effective.ExecutorAllowlist

	if task.Executor != "" {
		return s.validate(task.Executor, allow, reg)
	}

	// Routing is active and the task declares a class: the class must
	// resolve to an eligible+allowlisted executor, or selection fails closed
	// — it never silently falls back to the default (docs/PLAN.md Task 90
	// acceptance).
	if len(s.Routing) > 0 && task.Class != "" {
		return s.routeClassed(task.Class, allow, reg)
	}

	if s.Default == "" {
		return "", &SelectionError{
			Reason:         ReasonNoExecutorConfigured,
			Classification: verify.ClassificationPolicyViolation,
		}
	}
	return s.validate(s.Default, allow, reg)
}

// validate applies the two hard gates — policy allowlist, then capability
// registry membership+support — to a candidate name.
func (s ExecutorSelector) validate(name string, allow []string, reg capability.Registry) (string, error) {
	if !containsString(allow, name) {
		return "", &SelectionError{Executor: name, Reason: ReasonNotInAllowlist, Classification: verify.ClassificationPolicyViolation}
	}
	rec, ok := reg.Lookup(name)
	if !ok {
		return "", &SelectionError{Executor: name, Reason: ReasonUnknownExecutor, Classification: verify.ClassificationPolicyViolation}
	}
	if rec.Availability != capability.AvailabilitySupported {
		return "", &SelectionError{Executor: name, Reason: ReasonUnsupportedExecutor, Classification: verify.ClassificationPolicyViolation}
	}
	return name, nil
}

func containsString(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// RoutingTable maps a task class (e.g. "architecture", "frontend") to an
// ordered list of preferred executor names. It is populated from
// config/executor-routing.yaml (docs/PLAN.md Task 90 / PRV-07). The Task 85
// baseline leaves it empty, so routing is inactive and Select falls through
// to Default.
type RoutingTable map[string][]string

// routeClassed resolves a routed task class to an executor. The first
// preference for the class that is BOTH allowlisted AND a supported registry
// record for s.Profile wins — deterministic tie-break by list order, no
// per-request LLM judgment. An unrouted class or a class with no eligible
// preference fails closed (never defaults), per Task 90's acceptance.
func (s ExecutorSelector) routeClassed(class string, allow []string, reg capability.Registry) (string, error) {
	prefs, ok := s.Routing[class]
	if !ok || len(prefs) == 0 {
		return "", &SelectionError{Reason: ReasonUnroutedClass, Classification: verify.ClassificationPolicyViolation}
	}
	eligible := reg.Eligible(s.Profile, nil)
	for _, name := range prefs {
		if !containsString(allow, name) {
			continue
		}
		for _, rec := range eligible {
			if rec.Provider == name {
				return name, nil
			}
		}
	}
	return "", &SelectionError{Reason: ReasonNoEligibleExecutor, Classification: verify.ClassificationPolicyViolation}
}
