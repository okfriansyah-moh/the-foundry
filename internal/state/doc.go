// Package state is the single source of workflow lifecycle truth (Constitution
// C1). It defines the six canonical workflow statuses, the registry-controlled
// Phase/Reason/ResultCode fields that carry all richer meaning, and the legal
// status-transition graph.
//
// Governing doc: docs/foundry/docs/architecture/state-model.md.
//
// This package has zero imports beyond the standard library and performs no
// persistence, no Temporal interaction, and defines no JSON API shape beyond
// the Transition record itself.
package state
