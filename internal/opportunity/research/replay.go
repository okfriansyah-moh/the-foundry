package research

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// Cassette is one recorded research outcome for a specific idea, keyed by idea
// ID so a replay whose idea does not match fails loudly instead of silently
// returning someone else's evidence.
type Cassette struct {
	IdeaID        string              `json:"idea_id"`
	Partial       bool                `json:"partial"`
	PartialReason string              `json:"partial_reason"`
	Claims        []opportunity.Claim `json:"claims"`
}

// ReplayResearcher is the deterministic, cassette-backed default Researcher.
// It is the only path CI ever runs: no network, byte-deterministic.
type ReplayResearcher struct {
	byIdea map[string]Cassette
}

// LoadReplayResearcher loads one or more cassette files, keyed by their
// idea_id. A duplicate idea_id across cassettes is an error.
func LoadReplayResearcher(paths ...string) (*ReplayResearcher, error) {
	r := &ReplayResearcher{byIdea: map[string]Cassette{}}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("research: read cassette %s: %w", p, err)
		}
		var c Cassette
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("research: decode cassette %s: %w", p, err)
		}
		if c.IdeaID == "" {
			return nil, fmt.Errorf("research: cassette %s: idea_id is required", p)
		}
		if _, dup := r.byIdea[c.IdeaID]; dup {
			return nil, fmt.Errorf("research: duplicate cassette idea_id %q (%s)", c.IdeaID, p)
		}
		r.byIdea[c.IdeaID] = c
	}
	return r, nil
}

// Propose returns the cassette's claims for the idea. A missing cassette is a
// loud error, not a silent empty set. When the cassette is marked partial, the
// claims are returned alongside a *PartialCycle error.
func (r *ReplayResearcher) Propose(_ context.Context, idea opportunity.Idea) ([]opportunity.Claim, error) {
	c, ok := r.byIdea[idea.ID]
	if !ok {
		return nil, fmt.Errorf("research: no cassette for idea %q", idea.ID)
	}
	out := make([]opportunity.Claim, len(c.Claims))
	copy(out, c.Claims)
	if c.Partial {
		reason := c.PartialReason
		if reason == "" {
			reason = "cap exhausted"
		}
		return out, &PartialCycle{Reason: reason}
	}
	return out, nil
}
