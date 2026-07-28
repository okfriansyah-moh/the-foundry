package memory

import (
	"context"
	"fmt"
	"time"
)

// Proposer turns source evidence into candidate memories. It is the LLM seam
// — a real implementation calls a model; tests use a cassette/static
// implementation so they are deterministic and never make a network call.
type Proposer interface {
	Propose(ctx context.Context, profile string, evidence []EvidenceInput) ([]Candidate, error)
}

// Curator runs the evidence → candidate → dedupe/merge → store pipeline
// (docs/PLAN.md Task 76). It never crosses profiles: every candidate must be
// scoped to the profile being curated, or Curate fails.
type Curator struct {
	// Proposer is the (LLM) candidate generator. Required.
	Proposer Proposer
	// Store persists curated memories. Required.
	Store Store
	// Vectors is the optional similarity index. Nil disables vector storage.
	Vectors VectorIndex
	// Embedder produces vectors for new memories. Nil disables vector
	// storage even if Vectors is set.
	Embedder Embedder
	// Now supplies the current time; defaults to time.Now when nil.
	Now func() time.Time
}

func (c *Curator) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// Curate generates candidate memories from evidence for exactly one profile,
// dedupes/merges them against what is already stored, persists the result,
// and (when an Embedder+VectorIndex are configured) indexes new memories. It
// returns the stored/merged memories in deterministic ID order.
func (c *Curator) Curate(ctx context.Context, profile string, evidence []EvidenceInput) ([]Memory, error) {
	if c.Proposer == nil || c.Store == nil {
		return nil, fmt.Errorf("memory: Curator requires a Proposer and a Store")
	}
	if profile == "" {
		return nil, fmt.Errorf("memory: Curate requires a profile")
	}

	candidates, err := c.Proposer.Propose(ctx, profile, evidence)
	if err != nil {
		return nil, fmt.Errorf("memory: propose candidates: %w", err)
	}

	// Merge candidates that normalize to the same content within this batch,
	// keyed by MemoryID, before touching the store.
	byID := map[string]Candidate{}
	order := []string{}
	for _, cand := range candidates {
		if cand.ProfileScope != profile {
			// Cross-profile write is impossible: a candidate scoped to a
			// different profile than the one being curated is a hard error.
			return nil, fmt.Errorf("memory: candidate scope %q != curate profile %q (cross-profile write refused)", cand.ProfileScope, profile)
		}
		if cand.Content == "" {
			return nil, fmt.Errorf("memory: candidate has empty content")
		}
		if len(cand.EvidenceRefs) == 0 {
			return nil, fmt.Errorf("memory: candidate %q has no evidence refs (provenance required)", cand.Content)
		}
		id := MemoryID(profile, cand.Content)
		if prev, ok := byID[id]; ok {
			prev.EvidenceRefs = mergeRefs(prev.EvidenceRefs, cand.EvidenceRefs)
			if cand.Confidence > prev.Confidence {
				prev.Confidence = cand.Confidence
			}
			if cand.TTL > prev.TTL {
				prev.TTL = cand.TTL
			}
			byID[id] = prev
		} else {
			byID[id] = cand
			order = append(order, id)
		}
	}

	var out []Memory
	for _, id := range order {
		cand := byID[id]
		m, err := c.storeCandidate(ctx, profile, id, cand)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// storeCandidate merges cand into an existing memory (if one exists for this
// profile+content) or creates a new one, then indexes it.
func (c *Curator) storeCandidate(ctx context.Context, profile, id string, cand Candidate) (Memory, error) {
	now := c.now()

	existing, found, err := c.Store.GetForProfile(ctx, profile, id)
	if err != nil {
		return Memory{}, fmt.Errorf("memory: lookup existing: %w", err)
	}

	var m Memory
	if found {
		m = existing
		m.EvidenceRefs = mergeRefs(existing.EvidenceRefs, cand.EvidenceRefs)
		if cand.Confidence > m.Confidence {
			m.Confidence = cand.Confidence
		}
		if cand.Kind != "" {
			m.Kind = cand.Kind
		}
		if cand.TTL > 0 && (m.TTL == 0 || cand.TTL > m.TTL) {
			m.TTL = cand.TTL
			m.ExpiresAt = m.CreatedAt.Add(cand.TTL)
		}
	} else {
		m = Memory{
			ID:           id,
			Content:      cand.Content,
			Kind:         cand.Kind,
			ProfileScope: profile,
			Confidence:   cand.Confidence,
			EvidenceRefs: mergeRefs(nil, cand.EvidenceRefs),
			TTL:          cand.TTL,
			CreatedAt:    now,
		}
		if cand.TTL > 0 {
			m.ExpiresAt = now.Add(cand.TTL)
		}
	}

	if err := c.Store.Put(ctx, m); err != nil {
		return Memory{}, fmt.Errorf("memory: store: %w", err)
	}

	if c.Vectors != nil && c.Embedder != nil {
		vec, err := c.Embedder.Embed(ctx, m.Content)
		if err != nil {
			return Memory{}, fmt.Errorf("memory: embed: %w", err)
		}
		if err := c.Vectors.Upsert(ctx, m.ID, vec); err != nil {
			return Memory{}, fmt.Errorf("memory: index vector: %w", err)
		}
	}
	return m, nil
}

// Retrieve returns the memories scoped to profile (cross-profile read
// impossible), optionally dropping any that have expired as of now. It is the
// per-profile retrieval API.
func Retrieve(ctx context.Context, store Store, profile string, now time.Time) ([]Memory, error) {
	all, err := store.ListByProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, m := range all {
		if !m.ExpiresAt.IsZero() && !now.Before(m.ExpiresAt) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// DeleteDerivedFrom is the deletion-cascade coordinator (docs/PLAN.md Task 76
// / Task 66 integration): it deletes every memory derived from evidence ref
// and, for each, removes its vector-index entry. Returns the deleted memory
// IDs. Derived knowledge never outlives its source.
func DeleteDerivedFrom(ctx context.Context, store Store, vectors VectorIndex, ref string) ([]string, error) {
	deleted, err := store.DeleteBySource(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("memory: delete by source %q: %w", ref, err)
	}
	if vectors != nil {
		for _, id := range deleted {
			if err := vectors.Delete(ctx, id); err != nil {
				return nil, fmt.Errorf("memory: delete vector %q: %w", id, err)
			}
		}
	}
	return deleted, nil
}
