// Package profile implements load/save for Foundry profiles
// (docs/PLAN.md Task 21 / FND-02) over the profiles table created by Task 20
// (internal/db/migrations/00005_profiles.sql): personal or organization
// records whose config jsonb is validated against a versioned JSONSchema
// (config/schemas/profile.schema.json) before it is ever persisted.
//
// Authority limits: this package performs no side effects outside its own
// Postgres-backed store implementation (PGStore) and makes no policy or
// admission decision — policy compilation from a profile's config is Task 22
// (internal/policy), a separate, kernel-owned concern (Constitution C4).
// This package never imports internal/scm/write.
package profile
