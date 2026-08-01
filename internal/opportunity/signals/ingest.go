package signals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrIneligible is returned when a record cannot satisfy the real-signal gate
// (missing provenance, synthetic/test, unallowlisted class). It is not a
// storage failure — the ingest may still persist for audit when requested.
var ErrIneligible = errors.New("signals: record ineligible for real-validation gate")

// ErrUnsolicitedOutreach is returned when an acquisition request lacks an
// authorized channel/audience/policy grant (Task 139 step 4).
var ErrUnsolicitedOutreach = errors.New("signals: unsolicited mass outreach is prohibited")

// ErrCapExceeded is returned when a bounded experiment would exceed its
// spend, duration, audience or event-volume caps.
var ErrCapExceeded = errors.New("signals: experiment cap exceeded")

// Store persists validation signals. Implementations must treat payload
// bytes as opaque and never rewrite them.
type Store interface {
	Put(ctx context.Context, s Signal) error
	Get(ctx context.Context, id string) (Signal, error)
	GetByIdempotencyKey(ctx context.Context, key string) (Signal, bool, error)
	ListForOpportunity(ctx context.Context, opportunityID string) ([]Signal, error)
}

// MemoryStore is an in-process store for unit tests.
type MemoryStore struct {
	mu    sync.Mutex
	byID  map[string]Signal
	byKey map[string]string
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]Signal{}, byKey: map[string]string{}}
}

func (m *MemoryStore) Put(_ context.Context, s Signal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == "" {
		return fmt.Errorf("signals: empty id")
	}
	if s.IdempotencyKey != "" {
		if existing, ok := m.byKey[s.IdempotencyKey]; ok && existing != s.ID {
			return fmt.Errorf("signals: idempotency key %q already bound to %s", s.IdempotencyKey, existing)
		}
		m.byKey[s.IdempotencyKey] = s.ID
	}
	cp := s
	if s.RawPayload != nil {
		cp.RawPayload = append([]byte(nil), s.RawPayload...)
	}
	m.byID[s.ID] = cp
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (Signal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return Signal{}, fmt.Errorf("signals: not found %s", id)
	}
	return s, nil
}

func (m *MemoryStore) GetByIdempotencyKey(_ context.Context, key string) (Signal, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byKey[key]
	if !ok {
		return Signal{}, false, nil
	}
	return m.byID[id], true, nil
}

func (m *MemoryStore) ListForOpportunity(_ context.Context, opportunityID string) ([]Signal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Signal, 0)
	for _, s := range m.byID {
		if s.OpportunityID == opportunityID {
			out = append(out, s)
		}
	}
	return out, nil
}

// IngestRequest is authenticated external-evidence intake. RawArtifact is
// preserved verbatim; summaries are derived labels, never substitutes.
type IngestRequest struct {
	OpportunityID  string
	Class          Class
	SourceIdentity string
	SourceRef      string
	ExperimentID   string
	Hypothesis     string
	SampleSize     int
	SampleDenom    int
	ObservedAt     time.Time
	AcquisitionUSD float64
	Currency       string
	Environment    Environment
	RawArtifact    []byte
	IdempotencyKey string
	// UntrustedText is external free text treated as data only — never
	// instructions that can alter allowlist/budget/verdict (C23).
	UntrustedText string
}

// sanitizeUntrusted strips common injection markers from external text so
// they cannot be mistaken for control directives. The original artifact
// bytes remain untouched in RawPayload.
func sanitizeUntrusted(s string) string {
	lower := strings.ToLower(s)
	for _, needle := range []string{
		"ignore previous", "ignore all instructions", "system:",
		"allowlist:", "must_have_real_validation_signal", "verdict:",
	} {
		if strings.Contains(lower, needle) {
			return "[redacted-untrusted-directive]"
		}
	}
	return s
}

// Ingest validates provenance, refuses unallowlisted classes for the real
// gate, and stores the record. Synthetic/test environments are accepted for
// mechanics tests but never count. Idempotent on IdempotencyKey.
func Ingest(ctx context.Context, store Store, allow Allowlist, req IngestRequest, now time.Time) (Signal, error) {
	if req.IdempotencyKey != "" {
		if existing, ok, err := store.GetByIdempotencyKey(ctx, req.IdempotencyKey); err != nil {
			return Signal{}, err
		} else if ok {
			return existing, nil
		}
	}
	if !req.Environment.Valid() {
		return Signal{}, fmt.Errorf("signals: invalid environment %q", req.Environment)
	}
	if !req.Class.Valid() {
		return Signal{}, fmt.Errorf("signals: unknown class %q", req.Class)
	}
	if req.Environment == EnvReal && !allow.Contains(req.Class) {
		return Signal{}, fmt.Errorf("%w: class %q not allowlisted", ErrIneligible, req.Class)
	}
	_ = sanitizeUntrusted(req.UntrustedText) // data only; never alters decision fields

	digest := DigestPayload(req.RawArtifact)
	id := req.IdempotencyKey
	if id == "" {
		id = digest
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s := Signal{
		ID:             id,
		OpportunityID:  req.OpportunityID,
		Class:          req.Class,
		SourceIdentity: req.SourceIdentity,
		SourceRef:      req.SourceRef,
		ExperimentID:   req.ExperimentID,
		Hypothesis:     req.Hypothesis,
		SampleSize:     req.SampleSize,
		SampleDenom:    req.SampleDenom,
		ObservedAt:     req.ObservedAt.UTC(),
		AcquisitionUSD: req.AcquisitionUSD,
		Currency:       req.Currency,
		Environment:    req.Environment,
		PayloadDigest:  digest,
		RawPayload:     append([]byte(nil), req.RawArtifact...),
		CreatedAt:      now.UTC(),
		IdempotencyKey: req.IdempotencyKey,
	}
	if !s.ProvenanceComplete() {
		return Signal{}, fmt.Errorf("%w: incomplete provenance", ErrIneligible)
	}
	if err := store.Put(ctx, s); err != nil {
		return Signal{}, err
	}
	return s, nil
}

// HasAllowlistedReal reports whether opportunityID has at least one eligible
// real signal — the RealSignalVerifier implementation Task 102 consumes.
func HasAllowlistedReal(ctx context.Context, store Store, allow Allowlist, opportunityID string) (bool, error) {
	list, err := store.ListForOpportunity(ctx, opportunityID)
	if err != nil {
		return false, err
	}
	for _, s := range list {
		if s.EligibleForRealGate(allow) {
			return true, nil
		}
	}
	return false, nil
}

// PGStore is a Postgres-backed Store (migration 00034_validation_signals).
type PGStore struct {
	DB *sql.DB
}

func (p *PGStore) Put(ctx context.Context, s Signal) error {
	_, err := p.DB.ExecContext(ctx, `
INSERT INTO validation_signals (
  id, opportunity_id, class, source_identity, source_ref, experiment_id, hypothesis,
  sample_size, sample_denominator, observed_at, acquisition_cost_usd, currency,
  environment, payload_digest, raw_payload, created_at, idempotency_key
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT (idempotency_key) WHERE idempotency_key <> '' DO NOTHING
`, s.ID, s.OpportunityID, string(s.Class), s.SourceIdentity, s.SourceRef, s.ExperimentID, s.Hypothesis,
		s.SampleSize, s.SampleDenom, s.ObservedAt, s.AcquisitionUSD, s.Currency,
		string(s.Environment), s.PayloadDigest, s.RawPayload, s.CreatedAt, nullEmpty(s.IdempotencyKey))
	return err
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (p *PGStore) Get(ctx context.Context, id string) (Signal, error) {
	row := p.DB.QueryRowContext(ctx, `
SELECT id, opportunity_id, class, source_identity, source_ref, experiment_id, hypothesis,
       sample_size, sample_denominator, observed_at, acquisition_cost_usd, currency,
       environment, payload_digest, raw_payload, created_at, COALESCE(idempotency_key,'')
FROM validation_signals WHERE id = $1`, id)
	return scanSignal(row)
}

func (p *PGStore) GetByIdempotencyKey(ctx context.Context, key string) (Signal, bool, error) {
	row := p.DB.QueryRowContext(ctx, `
SELECT id, opportunity_id, class, source_identity, source_ref, experiment_id, hypothesis,
       sample_size, sample_denominator, observed_at, acquisition_cost_usd, currency,
       environment, payload_digest, raw_payload, created_at, COALESCE(idempotency_key,'')
FROM validation_signals WHERE idempotency_key = $1`, key)
	s, err := scanSignal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Signal{}, false, nil
	}
	if err != nil {
		return Signal{}, false, err
	}
	return s, true, nil
}

func (p *PGStore) ListForOpportunity(ctx context.Context, opportunityID string) ([]Signal, error) {
	rows, err := p.DB.QueryContext(ctx, `
SELECT id, opportunity_id, class, source_identity, source_ref, experiment_id, hypothesis,
       sample_size, sample_denominator, observed_at, acquisition_cost_usd, currency,
       environment, payload_digest, raw_payload, created_at, COALESCE(idempotency_key,'')
FROM validation_signals WHERE opportunity_id = $1 ORDER BY created_at ASC`, opportunityID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Signal
	for rows.Next() {
		s, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSignal(row scannable) (Signal, error) {
	var s Signal
	var class, env string
	var raw []byte
	err := row.Scan(
		&s.ID, &s.OpportunityID, &class, &s.SourceIdentity, &s.SourceRef, &s.ExperimentID, &s.Hypothesis,
		&s.SampleSize, &s.SampleDenom, &s.ObservedAt, &s.AcquisitionUSD, &s.Currency,
		&env, &s.PayloadDigest, &raw, &s.CreatedAt, &s.IdempotencyKey,
	)
	if err != nil {
		return Signal{}, err
	}
	s.Class = Class(class)
	s.Environment = Environment(env)
	s.RawPayload = raw
	return s, nil
}

// ExperimentCaps bound an optional acquisition connector.
type ExperimentCaps struct {
	MaxSpendUSD       float64
	MaxDuration       time.Duration
	MaxAudience       int
	MaxEvents         int
	AuthorizedChannel string
	PolicyGrant       string
}

// AcquisitionRequest is the kernel-owned acquisition input.
type AcquisitionRequest struct {
	OpportunityID  string
	Class          Class
	Caps           ExperimentCaps
	SpendSoFar     float64
	EventsSoFar    int
	AudienceSoFar  int
	StartedAt      time.Time
	Now            time.Time
	IdempotencyKey string
	// CallerOverrideAllowlist is rejected when non-empty — prompt injection
	// / lying executor cannot widen the allowlist.
	CallerOverrideAllowlist json.RawMessage
}

// ValidateAcquisition refuses unsolicited outreach and cap breaches before
// any external call.
func ValidateAcquisition(req AcquisitionRequest) error {
	if len(req.CallerOverrideAllowlist) > 0 {
		return fmt.Errorf("signals: caller cannot override allowlist")
	}
	if req.Caps.AuthorizedChannel == "" || req.Caps.PolicyGrant == "" {
		return ErrUnsolicitedOutreach
	}
	if req.Caps.MaxSpendUSD > 0 && req.SpendSoFar >= req.Caps.MaxSpendUSD {
		return fmt.Errorf("%w: spend", ErrCapExceeded)
	}
	if req.Caps.MaxEvents > 0 && req.EventsSoFar >= req.Caps.MaxEvents {
		return fmt.Errorf("%w: events", ErrCapExceeded)
	}
	if req.Caps.MaxAudience > 0 && req.AudienceSoFar >= req.Caps.MaxAudience {
		return fmt.Errorf("%w: audience", ErrCapExceeded)
	}
	if req.Caps.MaxDuration > 0 && !req.StartedAt.IsZero() {
		now := req.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if now.Sub(req.StartedAt) >= req.Caps.MaxDuration {
			return fmt.Errorf("%w: duration", ErrCapExceeded)
		}
	}
	return nil
}
