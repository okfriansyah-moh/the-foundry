package intake

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// newID mints a random, prefixed identifier (e.g. "intake-<hex>").
func newID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("intake: generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
}

// Stage names the position of an intake run in its ordered pipeline. The seven
// forward stages run in the listed order; the two terminal-by-design stages end
// a run cleanly at stage 2 and are successes, not failures.
type Stage string

// The ordered forward stages of the intake pipeline.
const (
	StageIdeaRecorded         Stage = "IDEA_RECORDED"
	StageOpportunityValidated Stage = "OPPORTUNITY_VALIDATED"
	StageSpecSynthesized      Stage = "SPEC_SYNTHESIZED"
	StagePlanGenerated        Stage = "PLAN_GENERATED"
	StageAdmitted             Stage = "ADMITTED"
	StageApproved             Stage = "APPROVED"
	StageMissionStarted       Stage = "MISSION_STARTED"

	// Terminal-by-design outcomes (not failures): the run ends at the verdict
	// gate having built nothing.
	StageOpportunityRejected           Stage = "OPPORTUNITY_REJECTED"
	StageOpportunityValidationRequired Stage = "OPPORTUNITY_VALIDATION_REQUIRED"

	// StageAwaitingStrongAuth records that an H-tier generated plan halted at
	// the approval gate awaiting strong-auth (C6/C12). The pipeline never
	// self-approves.
	StageAwaitingStrongAuth Stage = "AWAITING_STRONG_AUTH"

	// StageAwaitingReadiness records that an unattended mission cannot start
	// until the operator supplies ceremony answers the pipeline could not
	// derive (C17). It is a pause, not a failure.
	StageAwaitingReadiness Stage = "AWAITING_READINESS"
)

// forwardStages is the ordered happy-path sequence used to advance a run.
var forwardStages = []Stage{
	StageIdeaRecorded,
	StageOpportunityValidated,
	StageSpecSynthesized,
	StagePlanGenerated,
	StageAdmitted,
	StageApproved,
	StageMissionStarted,
}

// Status is the coarse lifecycle state of a run: running, done (reached a
// terminal stage — started OR built-nothing), or paused (awaiting an external
// action such as strong-auth or ceremony answers).
type Status string

// Run lifecycle statuses.
const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusPaused  Status = "paused"
)

// Caps records the three budget figures an intake run is bound by. --budget
// establishes the mission envelope BEFORE stage 2 spends anything; the research
// cap and MVP cap are derived from it and recorded so a later stage cannot
// silently widen them.
type Caps struct {
	EnvelopeUSD    float64 `json:"envelope_usd"`
	ResearchCapUSD float64 `json:"research_cap_usd"`
	MVPCapUSD      float64 `json:"mvp_cap_usd"`
}

// DefaultResearchFraction is the fraction of the mission envelope reserved for
// opportunity validation (stage 2) by default. Recorded as a decision
// (no-gaps rule) rather than left implicit: research is a small, bounded probe
// before any build commitment.
const DefaultResearchFraction = 0.2

// CapsFromBudget derives the recorded caps from the operator-supplied envelope.
// The research cap is a bounded fraction of the envelope; the MVP cap is the
// remainder available to the build. A non-positive envelope yields zero caps,
// which the budget gate treats as "no envelope" and refuses (Task 119).
func CapsFromBudget(envelopeUSD float64) Caps {
	if envelopeUSD <= 0 {
		return Caps{}
	}
	research := envelopeUSD * DefaultResearchFraction
	return Caps{
		EnvelopeUSD:    envelopeUSD,
		ResearchCapUSD: research,
		MVPCapUSD:      envelopeUSD - research,
	}
}

// Origin records where an intake run came from, so a chat-originated run
// (Task 113) is distinguishable from a CLI run and carries its provenance. All
// fields are optional for a CLI run.
type Origin struct {
	// Channel is "cli" or "telegram".
	Channel string `json:"channel"`
	// PrincipalID is the authenticated principal that initiated the run.
	PrincipalID string `json:"principal_id,omitempty"`
	// ChatID is the originating Telegram chat, when Channel == "telegram".
	ChatID int64 `json:"chat_id,omitempty"`
	// MessageHash is the sha256 of the raw originating message (data, never an
	// instruction — Task 113 / LLM01).
	MessageHash string `json:"message_hash,omitempty"`
}

// Run is one intake run: an idea, its budget caps, its current stage/status and
// its provenance. Idea text is untrusted data end to end.
type Run struct {
	ID           string    `json:"id"`
	Idea         string    `json:"idea"`
	Caps         Caps      `json:"caps"`
	Origin       Origin    `json:"origin"`
	CurrentStage Stage     `json:"current_stage"`
	Status       Status    `json:"status"`
	SpentUSD     float64   `json:"spent_usd"`
	MissionID    string    `json:"mission_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StageRecord is the persisted result of one completed stage: the digest of the
// inputs that produced it, a reference/serialization of its output artifact, the
// cost it charged, and when it completed. Its presence makes a re-run of that
// stage idempotent.
type StageRecord struct {
	RunID       string    `json:"run_id"`
	Stage       Stage     `json:"stage"`
	InputDigest string    `json:"input_digest"`
	Output      []byte    `json:"output"`
	CostUSD     float64   `json:"cost_usd"`
	CreatedAt   time.Time `json:"created_at"`
}

// digest returns the hex sha256 of parts joined by a NUL separator. Used to
// bind a stage record to the exact inputs that produced it.
func digest(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
