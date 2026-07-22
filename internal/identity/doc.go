// Package identity implements the identity substrate for Delivery Foundry
// (docs/PLAN.md Task 21 / FND-02): typed Go views and CRUD over the
// principals, organizations, and org_members tables created by Task 20
// (internal/db/migrations/00004_principals.sql).
//
// Authority limits: this package performs no side effects outside its own
// Postgres-backed store implementation (PGStore) and makes no admission,
// policy, or dispatch decisions — those are kernel-owned (Constitution C4).
// It never imports internal/scm/write. Consumers (internal/profile, the
// policy compiler in Task 22, the CLI) depend on the Store interface
// defined here, not on a concrete implementation.
package identity
