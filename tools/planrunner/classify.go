package main

import (
	"regexp"
	"strings"
)

// Tier is the AUTO/GATED split this runner reuses verbatim from the A0/A1/A2/H
// admission-tier pattern (docs/foundry/docs/autonomy/admission-tiers.md), applied here
// to each card's own Risk/Rev fields instead of a new classifier — Task 3 Step 3, and
// Constitution C6 (a plan/task can never self-classify, and neither can this runner).
type Tier int

const (
	Unknown Tier = iota
	Auto
	Gated
)

func (t Tier) String() string {
	switch t {
	case Auto:
		return "AUTO"
	case Gated:
		return "GATED"
	default:
		return "UNKNOWN"
	}
}

var revValuePattern = regexp.MustCompile(`(?i)R[1-4]`)

// Classify implements Task 3 Step 3 exactly:
// Risk ∈ {Low, Med} AND Rev ∈ {R1, R2} -> AUTO.
// Risk = High OR Rev ∈ {R3, R4} -> GATED.
// It returns the tier and, for GATED, the reason to surface in the Telegram message.
func Classify(risk, rev string) (Tier, string) {
	r := strings.ToLower(firstWord(risk))
	revNorm := strings.ToUpper(revValuePattern.FindString(rev))

	isLowMed := r == "low" || r == "med" || r == "medium"
	isR1R2 := revNorm == "R1" || revNorm == "R2"

	if isLowMed && isR1R2 {
		return Auto, ""
	}

	switch {
	case r == "high" && (revNorm == "R3" || revNorm == "R4"):
		return Gated, "Risk: " + risk + ", Rev: " + revNorm
	case r == "high":
		return Gated, "Risk: " + risk
	default:
		return Gated, "Rev: " + revNorm
	}
}

func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
