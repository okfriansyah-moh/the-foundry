-- docs/PLAN.md Task 76 (EVO-03) — curated, provenance-stamped, deletable memory.
-- +goose Up
CREATE TABLE IF NOT EXISTS memories (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    kind          TEXT NOT NULL,
    profile_scope TEXT NOT NULL,
    confidence    DOUBLE PRECISION NOT NULL DEFAULT 0,
    ttl_seconds   BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ
);
-- Provenance: which source evidence each memory derives from. ON DELETE
-- CASCADE means deleting a memory removes its evidence links; the curator's
-- DeleteBySource deletes the memory rows themselves when a source is purged.
CREATE TABLE IF NOT EXISTS memory_evidence (
    memory_id    TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    evidence_ref TEXT NOT NULL,
    PRIMARY KEY (memory_id, evidence_ref)
);
-- Optional vector index (behind internal/memory.VectorIndex). Stored as bytea
-- so this migration needs no pgvector extension; a pgvector-backed
-- implementation can replace this table without changing the Go interface.
-- ON DELETE CASCADE guarantees deleting a memory deletes its vector
-- (delete-with-source proof, Task 76 acceptance).
CREATE TABLE IF NOT EXISTS memory_vectors (
    memory_id TEXT PRIMARY KEY REFERENCES memories(id) ON DELETE CASCADE,
    embedding BYTEA NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_profile ON memories(profile_scope);
CREATE INDEX IF NOT EXISTS idx_memory_evidence_ref ON memory_evidence(evidence_ref);
-- +goose Down
DROP TABLE IF EXISTS memory_vectors;
DROP TABLE IF EXISTS memory_evidence;
DROP TABLE IF EXISTS memories;
