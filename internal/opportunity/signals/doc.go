// Package signals is the provenance-backed real-market validation signal
// store (docs/PLAN.md Task 139 / OPP-05). Ingestion proposes evidence only;
// acquisition side effects are kernel-owned. Synthetic/test events never
// satisfy must_have_real_validation_signal (Constitution C23).
package signals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Environment is the closed set of signal environments.
type Environment string

const (
	EnvReal      Environment = "real"
	EnvSynthetic Environment = "synthetic"
	EnvTest      Environment = "test"
)

// Valid reports whether e is a recognized environment.
func (e Environment) Valid() bool {
	switch e {
	case EnvReal, EnvSynthetic, EnvTest:
		return true
	default:
		return false
	}
}

// Class is one allowlisted evidence class eligible for
// must_have_real_validation_signal when fully provenanced and EnvReal.
type Class string

const (
	ClassLandingConversion Class = "landing_page_conversion"
	ClassWaitlistSignup    Class = "waitlist_signup"
	ClassPricingCTA        Class = "pricing_cta"
	ClassQualifiedInbound  Class = "qualified_inbound_interest"
	ClassTrafficExperiment Class = "bounded_traffic_experiment"
	ClassInterviewProspect Class = "interview_prospect_evidence"
)

// Valid reports whether c is a known class name (allowlist membership is
// separate — see Allowlist.Contains).
func (c Class) Valid() bool {
	switch c {
	case ClassLandingConversion, ClassWaitlistSignup, ClassPricingCTA,
		ClassQualifiedInbound, ClassTrafficExperiment, ClassInterviewProspect:
		return true
	default:
		return false
	}
}

// Signal is one immutable validation-signal record. Missing provenance
// fields make the record ineligible rather than partially trusted.
type Signal struct {
	ID             string      `json:"id"`
	OpportunityID  string      `json:"opportunity_id"`
	Class          Class       `json:"class"`
	SourceIdentity string      `json:"source_identity"`
	SourceRef      string      `json:"source_ref"`
	ExperimentID   string      `json:"experiment_id"`
	Hypothesis     string      `json:"hypothesis"`
	SampleSize     int         `json:"sample_size"`
	SampleDenom    int         `json:"sample_denominator"`
	ObservedAt     time.Time   `json:"observed_at"`
	AcquisitionUSD float64     `json:"acquisition_cost_usd"`
	Currency       string      `json:"currency"`
	Environment    Environment `json:"environment"`
	PayloadDigest  string      `json:"payload_digest"`
	RawPayload     []byte      `json:"-"` // preserved verbatim; never substituted
	CreatedAt      time.Time   `json:"created_at"`
	IdempotencyKey string      `json:"idempotency_key"`
}

// ProvenanceComplete reports whether every field required for eligibility is
// present. Incomplete records are stored for audit but never count.
func (s Signal) ProvenanceComplete() bool {
	if s.OpportunityID == "" || !s.Class.Valid() || s.SourceIdentity == "" || s.SourceRef == "" {
		return false
	}
	if s.ExperimentID == "" || s.Hypothesis == "" {
		return false
	}
	if s.SampleSize <= 0 || s.SampleDenom <= 0 || s.SampleSize > s.SampleDenom {
		return false
	}
	if s.ObservedAt.IsZero() || s.PayloadDigest == "" || !s.Environment.Valid() {
		return false
	}
	if s.Currency == "" && s.AcquisitionUSD != 0 {
		return false
	}
	return true
}

// EligibleForRealGate reports whether this record may satisfy
// must_have_real_validation_signal: allowlisted class, EnvReal, complete
// provenance. Synthetic/test never qualify.
func (s Signal) EligibleForRealGate(allow Allowlist) bool {
	if s.Environment != EnvReal {
		return false
	}
	if !s.ProvenanceComplete() {
		return false
	}
	return allow.Contains(s.Class)
}

// DigestPayload returns the immutable SHA-256 hex digest of raw.
func DigestPayload(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// MarshalCanonical returns a stable JSON form used for idempotency hashing
// of request metadata (not the raw artifact).
func MarshalCanonical(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("signals: marshal canonical: %w", err)
	}
	return b, nil
}
