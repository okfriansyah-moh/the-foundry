package research

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// DefaultConfigPath is the canonical location of the research policy config.
const DefaultConfigPath = "config/opportunity-research.yaml"

// SourcePolicy is the allowed/blocked domain policy handed to the provider's
// web tools as allowed_domains/blocked_domains.
type SourcePolicy struct {
	AllowedDomains []string `yaml:"allowed_domains"`
	BlockedDomains []string `yaml:"blocked_domains"`
}

// PerCycle bounds a single research cycle.
type PerCycle struct {
	MaxSearches int     `yaml:"max_searches"`
	MaxFetches  int     `yaml:"max_fetches"`
	TokenCap    int     `yaml:"token_cap"`
	DollarCap   float64 `yaml:"dollar_cap"`
}

// Config is the parsed research policy.
type Config struct {
	Version          string       `yaml:"version"`
	SourcePolicy     SourcePolicy `yaml:"source_policy"`
	PerCycle         PerCycle     `yaml:"per_cycle"`
	InjectionMarkers []string     `yaml:"injection_markers"`
}

// LoadConfig reads and validates the research policy config.
func LoadConfig(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("research: read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("research: decode config %s: %w", path, err)
	}
	if strings.TrimSpace(c.Version) == "" {
		return Config{}, fmt.Errorf("research: config %s: version is required", path)
	}
	if c.PerCycle.DollarCap <= 0 {
		return Config{}, fmt.Errorf("research: config %s: per_cycle.dollar_cap must be > 0", path)
	}
	return c, nil
}

// Researcher is the provider seam for opportunity evidence, mirroring
// spec.CandidateSource's shape. Propose may return a *PartialCycle error
// alongside a partial claim set when a cap is exhausted; that is a normal
// outcome, not a failure.
type Researcher interface {
	Propose(ctx context.Context, idea opportunity.Idea) ([]opportunity.Claim, error)
}

// PartialCycle signals that a research cycle ended early because a budget or a
// provider cap (max_uses/token/dollar) was exhausted. The claims returned
// alongside it are honest and partial, never a claim that the search space was
// covered.
type PartialCycle struct {
	Reason string
}

func (e *PartialCycle) Error() string { return "research: partial cycle: " + e.Reason }

// ArtifactResolver resolves a claim's SourceRef to the content hash of a
// stored, hash-verified artifact. A SourceRef that does not resolve is
// treated as fabricated: the claim can never be Observed.
type ArtifactResolver interface {
	Resolve(ctx context.Context, sourceRef string) (hash string, ok bool)
}

// noArtifacts resolves nothing — every SourceRef is treated as unverified. It
// is the fail-closed default when no resolver is supplied.
type noArtifacts struct{}

func (noArtifacts) Resolve(context.Context, string) (string, bool) { return "", false }

// BudgetReserver reserves a research cycle's dollar envelope against the cost
// ledger before any search runs. Keeping it an interface keeps this package
// free of a database dependency and deterministically testable.
type BudgetReserver interface {
	Reserve(ctx context.Context, amountUSD float64, meta any) (reservationID string, err error)
}

// CycleResult is the honest, fully-contained output of one research cycle.
type CycleResult struct {
	Claims        []opportunity.Claim
	Refused       []Refusal
	Partial       bool
	Reason        string
	ReservationID string
}

// RunCycle orchestrates one research cycle: it reserves the dollar envelope,
// invokes the researcher, applies containment, and appends the Skeptic's
// reject candidates. A partial cycle (cap exhausted) is returned with an
// explicit Unresolved marker rather than a silent continue.
//
// RunCycle never calls opportunity.Score or opportunity.Decide — proposing is
// strictly separated from deciding (Constitution C23).
func RunCycle(ctx context.Context, r Researcher, idea opportunity.Idea, cfg Config, reserver BudgetReserver, resolver ArtifactResolver) (CycleResult, error) {
	if r == nil {
		return CycleResult{}, fmt.Errorf("research: researcher is required")
	}
	if resolver == nil {
		resolver = noArtifacts{}
	}

	var res CycleResult

	// Reserve the envelope before any search (Task 101 Step 4). A failed
	// reservation ends the cycle before spending anything.
	if reserver != nil {
		id, err := reserver.Reserve(ctx, cfg.PerCycle.DollarCap, map[string]string{"idea": idea.ID, "purpose": "opportunity-research"})
		if err != nil {
			return CycleResult{}, fmt.Errorf("research: reserve budget: %w", err)
		}
		res.ReservationID = id
	}

	claims, err := r.Propose(ctx, idea)
	var partial *PartialCycle
	if errors.As(err, &partial) {
		res.Partial = true
		res.Reason = partial.Reason
	} else if err != nil {
		return CycleResult{}, fmt.Errorf("research: propose: %w", err)
	}

	kept, refused := Contain(ctx, claims, cfg, resolver)
	res.Claims = kept
	res.Refused = refused

	// A partial cycle carries an explicit Unresolved marker so downstream code
	// can never mistake it for full coverage.
	if res.Partial {
		res.Claims = append(res.Claims, opportunity.Claim{
			Kind:       opportunity.KindRisk,
			Text:       "research cycle ended early (" + res.Reason + "); evidence coverage is incomplete",
			Label:      opportunity.LabelUnresolved,
			Basis:      "partial research cycle",
			Untrusted:  true,
			ObservedAt: time.Time{},
		})
	}

	// The Skeptic re-reads the contained claim set and emits reject candidates
	// only — it can lower confidence, never raise it.
	res.Claims = append(res.Claims, Skeptic{}.Review(res.Claims)...)

	return res, nil
}
