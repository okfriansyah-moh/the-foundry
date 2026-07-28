package mission

import (
	"fmt"
	"sort"
	"strings"
)

// PortfolioMission is one supervised mission within a portfolio. Each carries
// its OWN budget envelope; spend in one mission can never draw down another's
// (budget isolation, per Task 29's per-mission cost scopes).
type PortfolioMission struct {
	ID               string
	MonthlyBudgetUSD float64
	SpentUSD         float64
	// RevenueBearing marks a mission whose product earns revenue; killing one
	// is a Tier-H decision requiring human approval (touchpoint inventory).
	RevenueBearing bool
	Active         bool
	// scheduled counts how many times this mission has been picked, used by
	// the fair scheduler to keep every active mission within one turn of
	// every other.
	scheduled int
}

// Portfolio supervises N missions, scheduling them fairly and keeping their
// budgets isolated, bounded by MaxActiveProducts (mission-contract.md
// `maximum_active_products`, Constitution C18). docs/PLAN.md Task 81 / EVO-08.
type Portfolio struct {
	MaxActiveProducts int
	missions          map[string]*PortfolioMission
	order             []string
}

// NewPortfolio returns an empty portfolio capped at maxActive concurrently
// active products (0 means unlimited).
func NewPortfolio(maxActive int) *Portfolio {
	return &Portfolio{MaxActiveProducts: maxActive, missions: map[string]*PortfolioMission{}}
}

// NewPortfolioFromQuota builds a portfolio whose active-mission cap is taken
// from a profile's control-plane quota (docs/PLAN.md Task 81 / EVO-08 — the
// portfolio honors Task 65's per-profile `max_active_missions` extension, so
// multi-mission fairness is bounded at both the mission-contract and the
// control-plane layer). A non-positive maxActiveMissions means unlimited.
func NewPortfolioFromQuota(maxActiveMissions int) *Portfolio {
	return NewPortfolio(maxActiveMissions)
}

// AddMission registers m. If m.Active is true it counts against
// MaxActiveProducts and is rejected when that cap is already reached.
func (p *Portfolio) AddMission(m PortfolioMission) error {
	if m.ID == "" {
		return fmt.Errorf("mission: portfolio mission requires an ID")
	}
	if _, exists := p.missions[m.ID]; exists {
		return fmt.Errorf("mission: %q already in portfolio", m.ID)
	}
	if m.Active && p.atActiveCap() {
		return fmt.Errorf("mission: cannot activate %q — maximum_active_products (%d) reached", m.ID, p.MaxActiveProducts)
	}
	cp := m
	p.missions[m.ID] = &cp
	p.order = append(p.order, m.ID)
	sort.Strings(p.order)
	return nil
}

func (p *Portfolio) atActiveCap() bool {
	return p.MaxActiveProducts > 0 && p.ActiveCount() >= p.MaxActiveProducts
}

// ActiveCount returns the number of currently active missions.
func (p *Portfolio) ActiveCount() int {
	n := 0
	for _, m := range p.missions {
		if m.Active {
			n++
		}
	}
	return n
}

// Activate marks a mission active, failing closed if that would exceed
// MaxActiveProducts.
func (p *Portfolio) Activate(id string) error {
	m, ok := p.missions[id]
	if !ok {
		return fmt.Errorf("mission: %q not in portfolio", id)
	}
	if m.Active {
		return nil
	}
	if p.atActiveCap() {
		return fmt.Errorf("mission: cannot activate %q — maximum_active_products (%d) reached", id, p.MaxActiveProducts)
	}
	m.Active = true
	return nil
}

// Deactivate marks a mission inactive.
func (p *Portfolio) Deactivate(id string) error {
	m, ok := p.missions[id]
	if !ok {
		return fmt.Errorf("mission: %q not in portfolio", id)
	}
	m.Active = false
	return nil
}

// Charge spends amount against id's OWN envelope only. It fails closed if the
// charge would exceed that mission's monthly budget — and it never touches
// any other mission's SpentUSD, so budget bleed between missions is
// impossible by construction.
func (p *Portfolio) Charge(id string, amount float64) error {
	m, ok := p.missions[id]
	if !ok {
		return fmt.Errorf("mission: %q not in portfolio", id)
	}
	if amount < 0 {
		return fmt.Errorf("mission: negative charge for %q", id)
	}
	if m.MonthlyBudgetUSD > 0 && m.SpentUSD+amount > m.MonthlyBudgetUSD {
		return fmt.Errorf("mission: charge $%.2f exceeds %q monthly budget ($%.2f remaining)", amount, id, m.MonthlyBudgetUSD-m.SpentUSD)
	}
	m.SpentUSD += amount
	return nil
}

// Remaining returns id's unspent monthly budget.
func (p *Portfolio) Remaining(id string) float64 {
	m, ok := p.missions[id]
	if !ok {
		return 0
	}
	return m.MonthlyBudgetUSD - m.SpentUSD
}

// NextScheduled returns the active mission that has been scheduled the fewest
// times (deterministic ID tie-break) and increments its count. This is the
// fair scheduler: no active mission is ever scheduled more than one turn
// ahead of any other, so a mission cannot starve its peers no matter how
// often it is offered work.
func (p *Portfolio) NextScheduled() (string, bool) {
	var pick *PortfolioMission
	for _, id := range p.order {
		m := p.missions[id]
		if !m.Active {
			continue
		}
		if pick == nil || m.scheduled < pick.scheduled {
			pick = m
		}
	}
	if pick == nil {
		return "", false
	}
	pick.scheduled++
	return pick.ID, true
}

// Schedule runs the fair scheduler for rounds turns and returns the sequence
// of scheduled mission IDs.
func (p *Portfolio) Schedule(rounds int) []string {
	out := make([]string, 0, rounds)
	for i := 0; i < rounds; i++ {
		id, ok := p.NextScheduled()
		if !ok {
			break
		}
		out = append(out, id)
	}
	return out
}

// FairnessSpread returns the difference between the most- and least-scheduled
// active missions. The fair scheduler guarantees this is at most 1.
func (p *Portfolio) FairnessSpread() int {
	minv, maxv := -1, -1
	for _, m := range p.missions {
		if !m.Active {
			continue
		}
		if minv < 0 || m.scheduled < minv {
			minv = m.scheduled
		}
		if m.scheduled > maxv {
			maxv = m.scheduled
		}
	}
	if minv < 0 {
		return 0
	}
	return maxv - minv
}

// PortfolioDecisionKind enumerates portfolio-level decide proposals.
type PortfolioDecisionKind string

const (
	DecisionInvestMore    PortfolioDecisionKind = "invest-more"
	DecisionHold          PortfolioDecisionKind = "hold"
	DecisionKillCandidate PortfolioDecisionKind = "kill-candidate"
)

// PortfolioDecision is a portfolio-level proposal. It is only ever a proposal
// — the kernel/human decides. Killing a revenue-bearing product always
// requires human approval (Tier H).
type PortfolioDecision struct {
	MissionID             string
	Kind                  PortfolioDecisionKind
	RequiresHumanApproval bool
	Rationale             string
}

// ProposeDecision builds a portfolio decision proposal. A kill-candidate for a
// revenue-bearing mission is forced to RequiresHumanApproval = true.
func (p *Portfolio) ProposeDecision(id string, kind PortfolioDecisionKind, rationale string) (PortfolioDecision, error) {
	m, ok := p.missions[id]
	if !ok {
		return PortfolioDecision{}, fmt.Errorf("mission: %q not in portfolio", id)
	}
	d := PortfolioDecision{MissionID: id, Kind: kind, Rationale: rationale}
	if kind == DecisionKillCandidate && m.RevenueBearing {
		d.RequiresHumanApproval = true
	}
	return d, nil
}

// MissionPanelRow is one mission's row in the portfolio dashboard/digest.
type MissionPanelRow struct {
	ID             string
	Active         bool
	SpentUSD       float64
	RemainingUSD   float64
	Scheduled      int
	RevenueBearing bool
}

// Panel returns the portfolio dashboard panel (deterministic ID order).
func (p *Portfolio) Panel() []MissionPanelRow {
	rows := make([]MissionPanelRow, 0, len(p.order))
	for _, id := range p.order {
		m := p.missions[id]
		rows = append(rows, MissionPanelRow{
			ID: m.ID, Active: m.Active, SpentUSD: m.SpentUSD,
			RemainingUSD: m.MonthlyBudgetUSD - m.SpentUSD, Scheduled: m.scheduled,
			RevenueBearing: m.RevenueBearing,
		})
	}
	return rows
}

// FormatPortfolioDigest renders the portfolio digest section.
func FormatPortfolioDigest(p *Portfolio) string {
	var b strings.Builder
	b.WriteString("*Portfolio*\n")
	fmt.Fprintf(&b, "Active %d / cap %d · fairness spread %d\n", p.ActiveCount(), p.MaxActiveProducts, p.FairnessSpread())
	for _, r := range p.Panel() {
		state := "idle"
		if r.Active {
			state = "active"
		}
		rev := ""
		if r.RevenueBearing {
			rev = " 💰"
		}
		fmt.Fprintf(&b, "  • %s (%s)%s — spent $%.2f, remaining $%.2f, scheduled %d\n",
			r.ID, state, rev, r.SpentUSD, r.RemainingUSD, r.Scheduled)
	}
	return b.String()
}
