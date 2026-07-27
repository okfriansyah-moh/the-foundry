// Package mission implements docs/PLAN.md Task 40 (VEN-01): the
// MissionContract engine (Constitution C18 -- missions are formal, bounded
// contracts with budgets, cadences, constraints, and exit semantics; a loop
// without an exit condition is an incident, not a feature).
//
// A MissionContract (contract.go) is parsed from YAML and validated against
// config/schemas/mission.schema.json (schema.go), field-for-field per
// docs/foundry/docs/autonomy/mission-contract.md §1 -- the schema is
// authoritative, never improvised. The evaluator (evaluator.go) is a pure
// function applying the contract's target/confirmation-window/budget/gate
// rules to a sequence of payment-provider-ledger observations and emitting
// mission result codes exactly as mapped in the governing doc's §2, via the
// existing internal/state registry (no new result-code plumbing is
// invented here -- state.ResultMissionTargetReached and its siblings
// already exist in that registry).
//
// MissionLoop (workflow.go) is the cron-cadenced Temporal workflow that
// orchestrates one mission's observe/evaluate/pause/terminate lifecycle. It
// only orchestrates: product delivery still goes through the existing
// internal/kernel.DeliverPlan workflow, invoked here as a child workflow,
// never duplicated.
//
// Authority: this package is go-kernel-owned scope (Constitution C4) --
// mission evaluation drives kernel-owned orchestration and is the only
// place a mission's own side effects (transitions, mission_state audit
// rows, gate_events, loop_contracts registration) are performed. It never
// imports internal/scm/write directly; SCM writes, if any mission-driven
// delivery ever needs one, remain routed through internal/kernel's own
// authority exclusively. It also implements no discovery/marketing logic
// (this task's own stated Boundary) -- "prohibited-market-detected" is
// accepted as an external signal, never itself detected here.
package mission
