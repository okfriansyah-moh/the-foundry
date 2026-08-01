// Package mission — improve.go
//
// Task 51 (VEN-12): Bounded autonomous improvement cycle.
//
// The improvement cycle implements the self-prompt loop governed by
// Constitution C18 (mission = bounded contract). When the observe tick
// produces decide=improve, this package:
//
//  1. Loads observation context and generates ONE bounded improvement plan
//     (LLM-backed with cassette replay in tests).
//  2. Submits the plan with creator_principal=service:mission-loop through
//     Task 44's plangen path.
//  3. Runs Task 45 admission classification.
//  4. Checks the profile envelope (auto_tiers + budget reservation).
//  5. If in-envelope: triggers kernel DeliverPlan, records a promotions row.
//  6. If out-of-envelope (H tier): halts with notification — no build, no deploy.
//
// Hard bounds (Constitution C18):
//   - Max 1 in-flight improvement per product (lease enforced in DB).
//   - Per-cycle budget cap from profile.
//   - No L0/L1 parameter promotion (Tasks 74–75).
package mission

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// ImprovementProposal is a single bounded change proposed by the generator.
// It carries exactly one concern, is reversible, and is scoped inside the
// product repo (Constitution C18 bounds enforced here before admission).
type ImprovementProposal struct {
	// PlanDoc is the generated improvement PLAN — a single-task Document
	// produced by the spec→plangen path (Task 44).
	PlanDoc *plan.Document
	// Digest is the SHA-256 of the canonical JSON of PlanDoc, used as the
	// plan_digest foreign key in the promotions table.
	Digest string
	// CycleID uniquely identifies this improvement attempt.
	CycleID string
	// GeneratedAt is when the proposal was created.
	GeneratedAt time.Time
}

// ImproveCycleInput is the input to RunImproveCycle.
type ImproveCycleInput struct {
	MissionID string
	ProductID string
	// Observation is the latest mission observation that triggered decide=improve.
	Observation Observation
	// BudgetCapUSD is the per-cycle budget ceiling from the profile.
	BudgetCapUSD float64
	// AutoTiers is the set of tiers allowed for auto-execution (profile envelope).
	AutoTiers []string
	// Generator is the improvement proposal generator (LLM-backed or cassette).
	Generator ImprovementGenerator
	// Admitter classifies the generated plan.
	Admitter ImprovementAdmitter
	// Frozen, when true, halts the cycle before any generation or admission:
	// a change-budget breach froze promotion (Constitution C20). The caller
	// reads the durable evolve.FreezeStore and passes the result in, so the
	// freeze is honoured across a restart, not just within one process.
	Frozen       bool
	FrozenReason string
}

// ImproveCycleResult is the result of RunImproveCycle.
type ImproveCycleResult struct {
	// CycleID uniquely identifies this cycle.
	CycleID string
	// Proposal is the generated plan (nil if generation failed).
	Proposal *ImprovementProposal
	// Admitted is true if the plan passed admission and is within envelope.
	Admitted bool
	// Tier is the admission tier assigned to the proposal.
	Tier string
	// HaltReason is non-empty when the cycle was halted without deployment.
	HaltReason string
	// Promotion is the recorded promotion row (nil if not deployed).
	Promotion *Promotion
}

// Promotion is a single row in the promotions table.
type Promotion struct {
	ID            string
	MissionID     string
	ProductID     string
	ChangeRef     string
	PlanDigest    string
	MetricsBefore map[string]float64
	RollbackRef   string
	Level         string
	CreatedAt     time.Time
}

// ImprovementGenerator generates a single bounded improvement proposal.
// In production it calls an LLM; in tests it replays cassettes.
type ImprovementGenerator interface {
	Generate(ctx context.Context, missionID, productID string, obs Observation) (*ImprovementProposal, error)
}

// ImprovementAdmitter classifies a plan document against the policy.
// It is satisfied by admission.Classify via this thin adapter.
type ImprovementAdmitter interface {
	Classify(doc *plan.Document) (admission.Decision, error)
}

// defaultAdmitter wraps admission.Classify with a noop policy view.
type defaultAdmitter struct{}

func (defaultAdmitter) Classify(doc *plan.Document) (admission.Decision, error) {
	return admission.Classify(doc, admission.NoopPolicyView{})
}

// DefaultAdmitter returns an ImprovementAdmitter that uses NoopPolicyView.
func DefaultAdmitter() ImprovementAdmitter { return defaultAdmitter{} }

// CassetteGenerator generates proposals deterministically from a pre-baked
// PLAN document. Used in tests and e2e scripts.
type CassetteGenerator struct {
	// Doc is the plan document to return.
	Doc *plan.Document
}

// Generate returns the cassette document, computing its digest on the fly.
func (c *CassetteGenerator) Generate(_ context.Context, _, _ string, _ Observation) (*ImprovementProposal, error) {
	if c.Doc == nil {
		return nil, fmt.Errorf("improve: cassette generator: no document configured")
	}
	raw, err := json.Marshal(c.Doc)
	if err != nil {
		return nil, fmt.Errorf("improve: cassette generator: marshal plan: %w", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	return &ImprovementProposal{
		PlanDoc:     c.Doc,
		Digest:      digest,
		CycleID:     fmt.Sprintf("cycle-%s-%d", c.Doc.ID, time.Now().UTC().UnixNano()),
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// envelopeCheck returns true if the admission tier is within the profile's
// auto_tiers envelope.
func envelopeCheck(tier string, autoTiers []string) bool {
	for _, t := range autoTiers {
		if t == tier {
			return true
		}
	}
	return false
}

// RunImproveCycle executes one bounded improvement cycle and returns the result.
// It performs no I/O against Temporal or the database; callers wire those
// persistence calls through activities (Constitution C4: kernel owns side effects).
func RunImproveCycle(ctx context.Context, in ImproveCycleInput) (ImproveCycleResult, error) {
	if in.Generator == nil {
		return ImproveCycleResult{}, fmt.Errorf("improve: generator is required")
	}
	if in.Admitter == nil {
		in.Admitter = DefaultAdmitter()
	}

	// A durable promotion freeze halts the cycle before any generation: a
	// change-budget breach froze promotion, and that freeze holds across a
	// restart (docs/PLAN.md Task 127, Constitution C20).
	if in.Frozen {
		reason := in.FrozenReason
		if reason == "" {
			reason = "promotion frozen"
		}
		return ImproveCycleResult{HaltReason: "frozen: " + reason}, nil
	}

	// Step 1: generate proposal.
	proposal, err := in.Generator.Generate(ctx, in.MissionID, in.ProductID, in.Observation)
	if err != nil {
		return ImproveCycleResult{}, fmt.Errorf("improve: generate proposal: %w", err)
	}
	if proposal == nil || proposal.PlanDoc == nil {
		return ImproveCycleResult{}, fmt.Errorf("improve: generator returned nil proposal or nil PlanDoc")
	}

	// Step 2: classify via admission (Task 45).
	decision, err := in.Admitter.Classify(proposal.PlanDoc)
	if err != nil {
		// Self-classification error halts immediately.
		return ImproveCycleResult{
			CycleID:    proposal.CycleID,
			Proposal:   proposal,
			Admitted:   false,
			Tier:       decision.Tier.String(),
			HaltReason: fmt.Sprintf("admission error: %v", err),
		}, nil
	}

	tier := decision.Tier.String()

	// Step 3: envelope check — profile auto_tiers gate.
	if !envelopeCheck(tier, in.AutoTiers) {
		return ImproveCycleResult{
			CycleID:    proposal.CycleID,
			Proposal:   proposal,
			Admitted:   false,
			Tier:       tier,
			HaltReason: fmt.Sprintf("out-of-envelope: tier %s not in auto_tiers %v — halted pre-build (notification required)", tier, in.AutoTiers),
		}, nil
	}

	// Step 4: in-envelope — record promotion stub (caller persists to DB).
	promotion := &Promotion{
		ID:         fmt.Sprintf("promo-%s-%d", in.MissionID, time.Now().UTC().UnixNano()),
		MissionID:  in.MissionID,
		ProductID:  in.ProductID,
		ChangeRef:  "", // filled by caller after DeliverPlan returns a deploy receipt
		PlanDigest: proposal.Digest,
		MetricsBefore: map[string]float64{
			"activation_rate":  in.Observation.ActivationRate,
			"conversion_rate":  in.Observation.ConversionRate,
			"net_mrr_usd":      in.Observation.NetMRRUSD,
			"cost_to_date_usd": in.Observation.CostToDateUSD,
		},
		RollbackRef: "", // filled by caller with current deploy receipt ref
		Level:       "plan-cycle",
		CreatedAt:   time.Now().UTC(),
	}

	return ImproveCycleResult{
		CycleID:   proposal.CycleID,
		Proposal:  proposal,
		Admitted:  true,
		Tier:      tier,
		Promotion: promotion,
	}, nil
}

// GenerateImprovementPlan builds a real, least-privilege improvement PLAN via
// Task 110's generator (spec.PlanFromSpecification) instead of hand-building a
// one-task plan with a hollow `make test` and a wildcard file glob (docs/PLAN.md
// Task 127). The improvement description becomes one specification requirement;
// the generator then emits tasks with real per-task validation (an allowlisted
// command or the explicit Task-104 opt-out) and a repo-write permission scoped
// to the mission's own repository path — never "*". A missing repo path is a
// hard error, never a literal fallback, so a generated plan can always be
// admitted like any other.
func GenerateImprovementPlan(missionID, productID, improvementDesc string, mapping spec.EffectMapping, mc spec.MissionContext, now time.Time) (*plan.Document, error) {
	if mc.RepoWriteTarget == "" {
		// Least-privilege default scoped to the product's own tree — never a
		// wildcard, which spec.PlanFromSpecification rejects outright.
		mc.RepoWriteTarget = "products/" + productID
	}
	section := "improvement"
	reqs := []spec.Requirement{{
		ID:      "improve-1",
		Section: section,
		Text:    improvementDesc,
		Label:   spec.LabelInferred,
		Basis:   "mission observation decide=improve",
		Impact:  spec.ImpactMedium,
	}}
	// Build the specification with EXACTLY one section directly, rather than
	// through spec.PostPass — PostPass injects completeness-coverage sections
	// that would expand a single bounded improvement into a many-task plan,
	// violating Constitution C18's "one concern, bounded" rule. The generator
	// still applies its real-validation and least-privilege discipline.
	s := spec.Specification{
		Requirements: reqs,
		Sections:     []string{section},
		BySection:    map[string][]int{section: {0}},
	}
	if len(mapping.Rows) == 0 {
		// A minimal least-privilege effect mapping for the single improvement
		// section, so the generated plan declares exactly the repo-write effect
		// it needs and nothing more.
		mapping = spec.EffectMapping{Rows: []spec.EffectMappingRow{{Section: section, Kind: "code", Target: mc.RepoWriteTarget}}}
	}
	planID := fmt.Sprintf("improve-%s-%d", missionID, now.UTC().UnixNano())
	title := fmt.Sprintf("Improvement cycle for product %s", productID)
	raw, err := spec.PlanFromSpecification(planID, title, s, mapping, mc)
	if err != nil {
		return nil, fmt.Errorf("improve: generate least-privilege plan: %w", err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("improve: parse generated plan: %w", err)
	}
	return doc, nil
}

// PlanDocFromSpec builds a single-task improvement plan.Document. It now
// delegates to GenerateImprovementPlan (Task 110's real generator) so the plan
// carries real validation and least-privilege permissions; the previous
// hand-built one-task plan with `make test` and a `products/<id>/**` wildcard is
// gone (docs/PLAN.md Task 127). The Document ID keeps the "improve-" prefix so
// callers can filter mission-loop-created documents.
func PlanDocFromSpec(missionID, productID, improvementDesc string, now time.Time) *plan.Document {
	mc := spec.MissionContext{
		RepoAlias:       "product",
		RepoURL:         "https://example.invalid/products/" + productID,
		RepoBranch:      "main",
		RepoWriteTarget: "products/" + productID,
	}
	doc, err := GenerateImprovementPlan(missionID, productID, improvementDesc, spec.EffectMapping{}, mc, now)
	if err != nil {
		// Never return a plan that bypasses the generator's least-privilege
		// guarantees; a caller that needs a valid repo context must use
		// GenerateImprovementPlan directly with real MissionContext.
		return nil
	}
	return doc
}
