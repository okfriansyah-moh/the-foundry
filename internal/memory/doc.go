// Package memory is the curated, provenance-stamped, deletable knowledge
// store (docs/PLAN.md Task 76 / EVO-03). Evidence goes in; curated memories
// come out, each stamped with the evidence refs it was derived from
// (provenance) and scoped to exactly one profile.
//
// Three invariants this package enforces:
//
//   - Provenance: every Memory carries the evidence refs it derives from;
//     nothing is stored without a source.
//   - Profile isolation: retrieval is per-profile — a memory scoped to one
//     profile is never returned to another (cross-profile read impossible).
//     The curator also refuses to write a candidate whose scope differs from
//     the profile it is curating for (cross-profile write impossible).
//   - Deletion cascade (Task 66 integration): deleting a source evidence ref
//     deletes every memory derived from it AND that memory's vector-index
//     entry — derived knowledge never outlives its source.
//
// The vector index is optional and lives behind the VectorIndex interface,
// so a pgvector-backed implementation can replace the in-memory one without
// touching the curator; delete-with-source holds for either.
package memory
