package kernel

import (
	"context"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity/signals"
)

// ActivityAcquireValidationSignal is the kernel-owned acquisition activity
// (docs/PLAN.md Task 139). Ingestion proposes evidence; only this activity
// may perform bounded external acquisition behind policy/budget/extops.
const (
	ActivityAcquireValidationSignal = "AcquireValidationSignal"
	ActivityIngestValidationSignal  = "IngestValidationSignal"
)

// StoreRealSignalVerifier implements RealSignalVerifier against a signal store.
type StoreRealSignalVerifier struct {
	Store     signals.Store
	Allowlist signals.Allowlist
}

// HasAllowlistedRealSignal reports whether opportunityID has an eligible real signal.
// A missing store is a named failure (Task 146), never a silent false that
// callers could treat as "no signal yet, keep going".
func (v StoreRealSignalVerifier) HasAllowlistedRealSignal(ctx context.Context, opportunityID string) (bool, error) {
	if v.Store == nil {
		return false, fmt.Errorf("kernel: StoreRealSignalVerifier missing signal store (production wiring refused)")
	}
	return signals.HasAllowlistedReal(ctx, v.Store, v.Allowlist, opportunityID)
}

// IngestValidationSignalInput is authenticated external evidence intake.
type IngestValidationSignalInput struct {
	WorkflowID     string                `json:"workflow_id"`
	IdempotencyKey string                `json:"idempotency_key"`
	Request        signals.IngestRequest `json:"request"`
}

// IngestValidationSignalOutput echoes the stored signal id and eligibility.
type IngestValidationSignalOutput struct {
	SignalID string `json:"signal_id"`
	Eligible bool   `json:"eligible_for_real_gate"`
	Digest   string `json:"payload_digest"`
}

// IngestValidationSignal stores a provenance-backed signal. It performs no
// external acquisition — that is AcquireValidationSignal.
func (a *Activities) IngestValidationSignal(ctx context.Context, in IngestValidationSignalInput) (IngestValidationSignalOutput, error) {
	if a.SignalStore == nil {
		return IngestValidationSignalOutput{}, fmt.Errorf("kernel: SignalStore not configured")
	}
	allow := a.SignalAllowlist
	if allow.Classes == nil {
		allow = signals.DefaultAllowlist()
	}
	s, err := signals.Ingest(ctx, a.SignalStore, allow, in.Request, time.Now().UTC())
	if err != nil {
		return IngestValidationSignalOutput{}, err
	}
	return IngestValidationSignalOutput{
		SignalID: s.ID,
		Eligible: s.EligibleForRealGate(allow),
		Digest:   s.PayloadDigest,
	}, nil
}

// AcquireValidationSignalInput is a bounded acquisition request.
type AcquireValidationSignalInput struct {
	WorkflowID string                     `json:"workflow_id"`
	Request    signals.AcquisitionRequest `json:"request"`
	// ResultArtifact is the verbatim export produced by the connector after
	// a successful bounded call (tests inject it; live adapters fill it).
	ResultArtifact []byte                `json:"result_artifact"`
	Ingest         signals.IngestRequest `json:"ingest"`
}

// AcquireValidationSignalOutput records the extops receipt + signal id.
type AcquireValidationSignalOutput struct {
	SignalID string `json:"signal_id"`
	Refused  bool   `json:"refused"`
	Reason   string `json:"reason"`
}

// AcquireValidationSignal fails closed before any external call when policy,
// channel, budget caps or credentials are absent; otherwise runs under
// WithExternalOp so retries cannot duplicate spend.
func (a *Activities) AcquireValidationSignal(ctx context.Context, in AcquireValidationSignalInput) (AcquireValidationSignalOutput, error) {
	if err := signals.ValidateAcquisition(in.Request); err != nil {
		return AcquireValidationSignalOutput{Refused: true, Reason: err.Error()}, nil
	}
	if a.ExternalOps == nil {
		return AcquireValidationSignalOutput{}, fmt.Errorf("kernel: ExternalOps required for validation-signal acquisition")
	}
	if a.SignalStore == nil {
		return AcquireValidationSignalOutput{}, fmt.Errorf("kernel: SignalStore not configured")
	}
	key := in.Request.IdempotencyKey
	if key == "" {
		key = "validation-signal:" + in.WorkflowID + ":" + in.Request.OpportunityID + ":" + string(in.Request.Class)
	}
	out, err := WithExternalOp(ctx, a.ExternalOps, in.WorkflowID, "validation.signal.acquire",
		in.Request.OpportunityID, key, in.Request,
		func(ctx context.Context) (AcquireValidationSignalOutput, error) {
			allow := a.SignalAllowlist
			if allow.Classes == nil {
				allow = signals.DefaultAllowlist()
			}
			ing := in.Ingest
			if len(ing.RawArtifact) == 0 {
				ing.RawArtifact = in.ResultArtifact
			}
			if ing.Environment == "" {
				ing.Environment = signals.EnvReal
			}
			if ing.OpportunityID == "" {
				ing.OpportunityID = in.Request.OpportunityID
			}
			if ing.Class == "" {
				ing.Class = in.Request.Class
			}
			if ing.IdempotencyKey == "" {
				ing.IdempotencyKey = key
			}
			s, err := signals.Ingest(ctx, a.SignalStore, allow, ing, time.Now().UTC())
			if err != nil {
				return AcquireValidationSignalOutput{}, err
			}
			return AcquireValidationSignalOutput{SignalID: s.ID}, nil
		})
	if err != nil {
		return AcquireValidationSignalOutput{}, err
	}
	return out, nil
}
