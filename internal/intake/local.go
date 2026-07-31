package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// FileOpportunityResolver resolves an idea to an Opportunity fixture on disk,
// keyed by the idea's digest. It is the zero-network resolver used by the
// cassette e2e (a live deployment substitutes the research intake path). The
// fixture file <dir>/<idea-digest>.json contains a marshaled
// opportunity.Opportunity; a missing fixture is a loud error, never a guess.
type FileOpportunityResolver struct {
	Dir string
}

// Resolve implements OpportunityResolver.
func (r FileOpportunityResolver) Resolve(_ context.Context, idea string) (opportunity.Opportunity, error) {
	key := digest(idea)
	path := filepath.Join(r.Dir, key+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return opportunity.Opportunity{}, fmt.Errorf("intake: opportunity fixture for idea digest %s: %w", key, err)
	}
	var opp opportunity.Opportunity
	if err := json.Unmarshal(raw, &opp); err != nil {
		return opportunity.Opportunity{}, fmt.Errorf("intake: decode opportunity fixture %s: %w", path, err)
	}
	return opp, nil
}

// FuncApprover adapts a function to the Approver seam. It must refuse an H-tier
// plan by returning ErrStrongAuthRequired; the pipeline also guards this, so a
// FuncApprover that forgets cannot self-approve an H-tier plan.
type FuncApprover func(ctx context.Context, in ApproveInput) (ApproveOutput, error)

// Approve implements Approver.
func (f FuncApprover) Approve(ctx context.Context, in ApproveInput) (ApproveOutput, error) {
	if strings.EqualFold(in.Tier, "H") {
		return ApproveOutput{}, ErrStrongAuthRequired
	}
	return f(ctx, in)
}

// FuncStarter adapts a function to the MissionStarter seam. Production injects a
// Temporal-backed starter; the cassette e2e injects a recording fake.
type FuncStarter func(ctx context.Context, in StartMissionInput) (StartMissionOutput, error)

// Start implements MissionStarter.
func (f FuncStarter) Start(ctx context.Context, in StartMissionInput) (StartMissionOutput, error) {
	return f(ctx, in)
}

// FuncReadiness adapts a function to the ReadinessChecker seam.
type FuncReadiness func(ctx context.Context, in ReadinessInput) (ReadinessOutput, error)

// Check implements ReadinessChecker.
func (f FuncReadiness) Check(ctx context.Context, in ReadinessInput) (ReadinessOutput, error) {
	return f(ctx, in)
}

// AlwaysReady is a ReadinessChecker for a run whose ceremony answers were all
// derivable (the mission repository, budget and approval are all known by the
// time stage 7 runs). A deployment with un-derivable ceremony inputs injects a
// checker that reports them as Missing instead.
var AlwaysReady ReadinessChecker = FuncReadiness(func(_ context.Context, in ReadinessInput) (ReadinessOutput, error) {
	return ReadinessOutput{Ready: true, ArtifactRef: "ceremony:" + in.RunID}, nil
})
