package evolve

import (
	"sync"
	"sync/atomic"
)

type FreezeCondition string

const (
	FreezeBudgetExceeded      FreezeCondition = "budget-exceeded"
	FreezeQualityRegression   FreezeCondition = "quality-regression"
	FreezeCostSpike           FreezeCondition = "cost-spike"
	FreezeSecurityClassChange FreezeCondition = "security-class-change"
	FreezeRollbackChainDepth  FreezeCondition = "rollback-chain-depth"
)

type ChangeBudgetLimits struct {
	MaxPromotions         int
	MaxFilesChanged       int
	MaxRoutingWeightDelta float64
	MaxCostDeltaUSD       float64
	MaxQualityRegression  float64
	MaxRollbackDepth      int
}

type BudgetWindow struct {
	Promotions           int
	FilesChanged         int
	RoutingWeightDelta   float64
	CostDeltaUSD         float64
	QualityDelta         float64
	RollbackChainDepth   int
	SecurityClassChanged bool
}

var frozen atomic.Bool
var freezeState struct {
	sync.Mutex
	reason FreezeCondition
}

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
	return out
}

func Freeze(reason FreezeCondition) {
	frozen.Store(true)
	freezeState.Lock()
	freezeState.reason = reason
	freezeState.Unlock()
}

func Unfreeze() {
	frozen.Store(false)
	freezeState.Lock()
	freezeState.reason = ""
	freezeState.Unlock()
}

func IsFrozen() bool { return frozen.Load() }

func FreezeReason() FreezeCondition {
	freezeState.Lock()
	defer freezeState.Unlock()
	return freezeState.reason
}
