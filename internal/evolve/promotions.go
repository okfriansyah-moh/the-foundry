package evolve

import "time"

type PromotionLevel string

type PromotionStage string

const (
	LevelL0 PromotionLevel = "L0"
	LevelL1 PromotionLevel = "L1"

	StageRejected    PromotionStage = "rejected"
	StageReplay      PromotionStage = "replay"
	StageShadow      PromotionStage = "shadow"
	StageCanary      PromotionStage = "canary"
	StagePromoted    PromotionStage = "promoted"
	StageReverted    PromotionStage = "reverted"
	StageQuarantined PromotionStage = "quarantined"
	StageProposed    PromotionStage = "proposed"
)

type PromotionRecord struct {
	Tunable       string
	PreviousValue float64
	PromotedValue float64
	RollbackRef   string
	Stage         PromotionStage
	Level         PromotionLevel
	CreatedAt     time.Time
}
