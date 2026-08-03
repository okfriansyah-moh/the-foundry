package evolve

import (
	"sync"
	"sync/atomic"
	"time"
)

type FreezeCondition string

const (
	FreezeBudgetExceeded      FreezeCondition = "budget-exceeded"
	FreezeQualityRegression   FreezeCondition = "quality-regression"
	FreezeCostSpike           FreezeCondition = "cost-spike"
	FreezeSecurityClassChange FreezeCondition = "security-class-change"
	FreezeRollbackChainDepth  FreezeCondition = "rollback-chain-depth"
)

// ChangeBudgetLimits defines the per-30d governance thresholds for C20.
// Placeholder is true when the concrete numbers are placeholders flagged for
// Blocker B7 resolution; conservative defaults are in effect until then.
type ChangeBudgetLimits struct {
	MaxPromotions              int
	MaxFilesChanged            int
	MaxRoutingWeightDelta      float64
	MaxCostDeltaUSD            float64
	MaxQualityRegression       float64
	MaxRollbackDepth           int
	MaxHumanCheckpointInterval time.Duration // freeze if time since last checkpoint exceeds this
	Placeholder                bool          // Blocker B7: conservative placeholders in effect
}

// DefaultChangeBudgetLimits returns conservative placeholder limits (C20).
// Placeholder is true until Blocker B7 is resolved with real telemetry numbers.
func DefaultChangeBudgetLimits() ChangeBudgetLimits {
	return ChangeBudgetLimits{
		MaxPromotions:              5,
		MaxFilesChanged:            50,
		MaxRoutingWeightDelta:      0.1,
		MaxCostDeltaUSD:            100,
		MaxQualityRegression:       0.05,
		MaxRollbackDepth:           2,
		MaxHumanCheckpointInterval: 7 * 24 * time.Hour,
		Placeholder:                true,
	}
}

// BudgetWindow tracks all dimensions of autonomous change within one 30-day
// rolling window (Constitution C20). TimeSinceHumanCheckpoint measures the
// elapsed wall time since the last human-approved checkpoint was recorded.
type BudgetWindow struct {
	Promotions               int
	FilesChanged             int
	RoutingWeightDelta       float64
	CostDeltaUSD             float64
	QualityDelta             float64
	RollbackChainDepth       int
	SecurityClassChanged     bool
	TimeSinceHumanCheckpoint time.Duration // zero means checkpoint is current
}

var (
	frozen atomic.Bool
	// activationFreezeMu closes the in-process check-to-activation race. A
	// promotion holds RLock from its final hot-latch check through catalog
	// activation; Freeze/Unfreeze take Lock before changing the latch.
	activationFreezeMu sync.RWMutex
	freezeState        struct {
		sync.Mutex
		reason FreezeCondition
		source freezeLatchSource
	}
)

type freezeLatchSource uint8

const (
	freezeLatchNone freezeLatchSource = iota
	freezeLatchLocal
	freezeLatchDurableMirror
)

func (w BudgetWindow) Breaches(limits ChangeBudgetLimits) []FreezeCondition {
	var out []FreezeCondition
	if limits.MaxPromotions > 0 && w.Promotions > limits.MaxPromotions || (limits.MaxFilesChanged > 0 && w.FilesChanged > limits.MaxFilesChanged) || (limits.MaxRoutingWeightDelta > 0 && w.RoutingWeightDelta > limits.MaxRoutingWeightDelta) {
		out = append(out, FreezeBudgetExceeded)
	}
	if limits.MaxCostDeltaUSD > 0 && w.CostDeltaUSD > limits.MaxCostDeltaUSD {
		out = append(out, FreezeCostSpike)
	}
	if limits.MaxQualityRegression > 0 && w.QualityDelta < 0 && -w.QualityDelta > limits.MaxQualityRegression {
		out = append(out, FreezeQualityRegression)
	}
	if w.SecurityClassChanged {
		out = append(out, FreezeSecurityClassChange)
	}
	if limits.MaxRollbackDepth > 0 && w.RollbackChainDepth > limits.MaxRollbackDepth {
		out = append(out, FreezeRollbackChainDepth)
	}
	// Time since human checkpoint: if the window has been running longer than
	// the configured maximum without a human checkpoint, treat it as a budget
	// breach requiring human intervention (C20).
	if limits.MaxHumanCheckpointInterval > 0 && w.TimeSinceHumanCheckpoint > limits.MaxHumanCheckpointInterval {
		out = append(out, FreezeBudgetExceeded)
	}
	return out
}

func Freeze(reason FreezeCondition) {
	activationFreezeMu.Lock()
	defer activationFreezeMu.Unlock()
	frozen.Store(true)
	freezeState.Lock()
	freezeState.reason = reason
	freezeState.source = freezeLatchLocal
	freezeState.Unlock()
}

// MirrorDurableFreeze refreshes the process-local fast-path view after the
// durable store has accepted a freeze. A direct local Freeze always wins over
// the mirror, so legacy in-process safety callers cannot be silently thawed by
// a durable-state refresh.
func MirrorDurableFreeze(reason FreezeCondition) {
	activationFreezeMu.Lock()
	defer activationFreezeMu.Unlock()
	freezeState.Lock()
	defer freezeState.Unlock()
	if freezeState.source == freezeLatchLocal {
		return
	}
	frozen.Store(true)
	freezeState.reason = reason
	freezeState.source = freezeLatchDurableMirror
}

// clearDurableFreezeMirror clears only a stale durable mirror. The caller must
// already hold a durable thawed promotion guard, which makes the refresh
// atomic with respect to durable Freeze/Unfreeze operations.
func clearDurableFreezeMirror() {
	activationFreezeMu.Lock()
	defer activationFreezeMu.Unlock()
	freezeState.Lock()
	defer freezeState.Unlock()
	if freezeState.source != freezeLatchDurableMirror {
		return
	}
	frozen.Store(false)
	freezeState.reason = ""
	freezeState.source = freezeLatchNone
}

func Unfreeze() {
	activationFreezeMu.Lock()
	defer activationFreezeMu.Unlock()
	frozen.Store(false)
	freezeState.Lock()
	freezeState.reason = ""
	freezeState.source = freezeLatchNone
	freezeState.Unlock()
}

func IsFrozen() bool { return frozen.Load() }

func FreezeReason() FreezeCondition {
	freezeState.Lock()
	defer freezeState.Unlock()
	return freezeState.reason
}
