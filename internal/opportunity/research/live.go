package research

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// Citation is one provider-returned source: the URL fetched and the content
// hash of the stored artifact. Together they form a claim's SourceRef.
type Citation struct {
	URL         string                `json:"url"`
	ContentHash string                `json:"content_hash"`
	Kind        opportunity.ClaimKind `json:"kind"`
	Text        string                `json:"text"`
	Label       opportunity.Label     `json:"label"`
}

// ResearchRequest is the parameterized call to the provider's server-side web
// tools. MaxSearches becomes the provider's web_search max_uses, so the cap is
// enforced provider-side as well as by our accounting.
type ResearchRequest struct {
	Idea           opportunity.Idea
	AllowedDomains []string
	BlockedDomains []string
	MaxSearches    int
	MaxFetches     int
}

// ResearchResponse is the provider's reply. A server-tool failure (e.g. a
// max_uses_exceeded cap) arrives here as ErrorObject on an otherwise
// successful response — never as a returned error — so cap exhaustion must be
// handled as a normal partial-cycle outcome (Task 101 Step 1).
type ResearchResponse struct {
	Citations   []Citation
	ErrorObject string
	TokensUsed  int
	DollarsUsed float64
}

// ProviderClient runs a single research request through the LLM provider's own
// web_search/web_fetch tools. It is an interface so LiveResearcher is testable
// without a live provider and so the concrete adapter lives with the other
// providers under internal/executor.
type ProviderClient interface {
	Research(ctx context.Context, req ResearchRequest) (ResearchResponse, error)
}

// ArtifactRecorder stores a fetched artifact so its URL+hash can later be
// resolved by an ArtifactResolver (closing the fabricated-SourceRef hole).
type ArtifactRecorder interface {
	Record(ctx context.Context, sourceURL, contentHash string, body []byte) error
}

// capExhaustionCodes are the provider error-object codes that mean "cap
// exhausted", which is a partial-cycle outcome, not a failure.
var capExhaustionCodes = map[string]bool{
	"max_uses_exceeded":      true,
	"token_budget_exceeded":  true,
	"rate_limit_exceeded":    true,
	"search_budget_exceeded": true,
}

// LiveResearcher is the gated (RUN_OPPORTUNITY_LIVE=1) Researcher that calls
// the provider's server-side web tools. It is first-party-API-only; a
// deployment pinned to Bedrock/Vertex must run cassette-only and say so.
type LiveResearcher struct {
	Provider ProviderClient
	Cfg      Config
	Recorder ArtifactRecorder // optional
	Now      func() time.Time // injectable clock; defaults to time.Now
}

// Propose runs one live research request and maps its citations to Untrusted
// claims. Cap exhaustion is returned as a partial cycle with whatever
// citations arrived, never as an empty set and never as an error.
func (l *LiveResearcher) Propose(ctx context.Context, idea opportunity.Idea) ([]opportunity.Claim, error) {
	if l.Provider == nil {
		return nil, fmt.Errorf("research: live researcher requires a provider")
	}
	if len(l.Cfg.SourcePolicy.AllowedDomains) == 0 {
		// Fail closed: a live cycle must name its sources.
		return nil, fmt.Errorf("research: source policy has no allowed_domains (fail-closed)")
	}
	now := l.Now
	if now == nil {
		now = time.Now
	}

	req := ResearchRequest{
		Idea:           idea,
		AllowedDomains: l.Cfg.SourcePolicy.AllowedDomains,
		BlockedDomains: l.Cfg.SourcePolicy.BlockedDomains,
		MaxSearches:    l.Cfg.PerCycle.MaxSearches,
		MaxFetches:     l.Cfg.PerCycle.MaxFetches,
	}
	resp, err := l.Provider.Research(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("research: provider call: %w", err)
	}

	var claims []opportunity.Claim
	for _, cit := range resp.Citations {
		if !domainAllowed(cit.URL, l.Cfg.SourcePolicy) {
			// A citation from outside the source policy is dropped here; even
			// if it survived, Contain would strip its unverifiable ref.
			continue
		}
		if l.Recorder != nil && cit.ContentHash != "" {
			// Best-effort: record the artifact so its ref later resolves.
			_ = l.Recorder.Record(ctx, cit.URL, cit.ContentHash, nil)
		}
		claims = append(claims, opportunity.Claim{
			Kind:       cit.Kind,
			Text:       cit.Text,
			Label:      cit.Label,
			Basis:      "provider web research",
			SourceRef:  citationRef(cit),
			ObservedAt: now(),
			Untrusted:  true,
		})
	}

	// A returned error object means cap exhaustion: honest partial cycle with
	// whatever we gathered — never confuse it with zero findings.
	if resp.ErrorObject != "" {
		if capExhaustionCodes[resp.ErrorObject] {
			return claims, &PartialCycle{Reason: resp.ErrorObject}
		}
		return claims, fmt.Errorf("research: provider error object %q", resp.ErrorObject)
	}
	return claims, nil
}

func citationRef(c Citation) string {
	if c.ContentHash == "" {
		return c.URL
	}
	return c.URL + "#" + c.ContentHash
}

// domainAllowed reports whether a URL's host is permitted by the source
// policy: not on the block-list and matching (host-or-subdomain) an entry on
// the non-empty allow-list.
func domainAllowed(rawURL string, policy SourcePolicy) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	for _, b := range policy.BlockedDomains {
		if hostMatches(host, b) {
			return false
		}
	}
	if len(policy.AllowedDomains) == 0 {
		return false
	}
	for _, a := range policy.AllowedDomains {
		if hostMatches(host, a) {
			return true
		}
	}
	return false
}

// hostMatches reports whether host equals domain or is a subdomain of it.
func hostMatches(host, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}
