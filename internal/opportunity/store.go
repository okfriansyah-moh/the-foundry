package opportunity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a lookup by ID finds no row.
var ErrNotFound = errors.New("opportunity: not found")

// Store is the Postgres-backed opportunity store
// (internal/db/migrations/00025_opportunities.sql). Evidence rows are
// append-only: the store exposes AppendEvidence and LoadEvidence but never an
// update or delete, so an evaluation's evidence trail cannot be rewritten.
type Store struct {
	db *sql.DB
}

// NewStore wraps an existing *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func newID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("opportunity: generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
}

// VerdictRecord is one stored verdict, bound to the exact scorecard,
// thresholds and config version that produced it, so it can never be
// re-explained after the fact with different weights.
type VerdictRecord struct {
	ID               string
	OpportunityID    string
	Verdict          Verdict
	UnmetThresholds  []string
	ScorecardDigest  string
	ThresholdsDigest string
	ConfigVersion    string
	Scorecard        Scorecard
	Thresholds       Thresholds
	CreatedAt        time.Time
}

// OpportunitySummary is a compact opportunity row for listing.
type OpportunitySummary struct {
	ID        string
	Statement string
	CreatedAt time.Time
}

// ListOpportunities returns the most recent opportunities, newest first. A
// non-positive limit defaults to 50.
func (s *Store) ListOpportunities(ctx context.Context, limit int) ([]OpportunitySummary, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, statement, created_at
FROM opportunities
ORDER BY created_at DESC, id DESC
LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("opportunity: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []OpportunitySummary
	for rows.Next() {
		var o OpportunitySummary
		if err := rows.Scan(&o.ID, &o.Statement, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("opportunity: scan list row: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("opportunity: iterate list: %w", err)
	}
	return out, nil
}

// CreateOpportunity persists the idea, ICP and economic envelope. It returns
// the opportunity ID (o.Idea.ID if set, otherwise a freshly minted one).
func (s *Store) CreateOpportunity(ctx context.Context, o Opportunity) (string, error) {
	id := o.Idea.ID
	if id == "" {
		var err error
		if id, err = newID("opp"); err != nil {
			return "", err
		}
	}
	icpJSON, err := json.Marshal(o.ICP)
	if err != nil {
		return "", fmt.Errorf("opportunity: encode icp: %w", err)
	}
	var submittedAt any
	if !o.Idea.SubmittedAt.IsZero() {
		submittedAt = o.Idea.SubmittedAt
	}
	const q = `
INSERT INTO opportunities
  (id, statement, submitted_by, submitted_at, source, icp,
   estimated_validation_cost_usd, mvp_budget_usd, max_active_builds, real_validation_signal)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	if _, err := s.db.ExecContext(ctx, q,
		id, o.Idea.Statement, o.Idea.SubmittedBy, submittedAt, o.Idea.Source, icpJSON,
		o.EstimatedValidationCostUSD, o.MVPBudgetUSD, o.MaxActiveBuilds, o.RealValidationSignal,
	); err != nil {
		return "", fmt.Errorf("opportunity: create %s: %w", id, err)
	}
	return id, nil
}

// AppendEvidence appends one claim to the append-only evidence log.
func (s *Store) AppendEvidence(ctx context.Context, oppID string, c Claim) error {
	id, err := newID("evi")
	if err != nil {
		return err
	}
	var observedAt any
	if !c.ObservedAt.IsZero() {
		observedAt = c.ObservedAt
	}
	const q = `
INSERT INTO opportunity_evidence
  (id, opportunity_id, kind, text, label, basis, source_ref, observed_at, untrusted)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	if _, err := s.db.ExecContext(ctx, q,
		id, oppID, string(c.Kind), c.Text, string(c.Label), c.Basis, c.SourceRef, observedAt, c.Untrusted,
	); err != nil {
		return fmt.Errorf("opportunity: append evidence to %s: %w", oppID, err)
	}
	return nil
}

// LoadEvidence returns the append-ordered evidence for an opportunity.
func (s *Store) LoadEvidence(ctx context.Context, oppID string) ([]Claim, error) {
	const q = `
SELECT kind, text, label, basis, source_ref, observed_at, untrusted
FROM opportunity_evidence
WHERE opportunity_id = $1
ORDER BY seq ASC`
	rows, err := s.db.QueryContext(ctx, q, oppID)
	if err != nil {
		return nil, fmt.Errorf("opportunity: load evidence for %s: %w", oppID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []Claim
	for rows.Next() {
		var c Claim
		var kind, label string
		var observedAt sql.NullTime
		if err := rows.Scan(&kind, &c.Text, &label, &c.Basis, &c.SourceRef, &observedAt, &c.Untrusted); err != nil {
			return nil, fmt.Errorf("opportunity: scan evidence for %s: %w", oppID, err)
		}
		c.Kind = ClaimKind(kind)
		c.Label = Label(label)
		if observedAt.Valid {
			c.ObservedAt = observedAt.Time
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("opportunity: iterate evidence for %s: %w", oppID, err)
	}
	return out, nil
}

// LoadOpportunity reconstructs an opportunity from its row plus its
// append-only evidence.
func (s *Store) LoadOpportunity(ctx context.Context, oppID string) (Opportunity, error) {
	const q = `
SELECT id, statement, submitted_by, submitted_at, source, icp,
       estimated_validation_cost_usd, mvp_budget_usd, max_active_builds, real_validation_signal
FROM opportunities WHERE id = $1`
	var o Opportunity
	var icpJSON []byte
	var submittedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, q, oppID).Scan(
		&o.Idea.ID, &o.Idea.Statement, &o.Idea.SubmittedBy, &submittedAt, &o.Idea.Source, &icpJSON,
		&o.EstimatedValidationCostUSD, &o.MVPBudgetUSD, &o.MaxActiveBuilds, &o.RealValidationSignal,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Opportunity{}, ErrNotFound
	}
	if err != nil {
		return Opportunity{}, fmt.Errorf("opportunity: load %s: %w", oppID, err)
	}
	if submittedAt.Valid {
		o.Idea.SubmittedAt = submittedAt.Time
	}
	if len(icpJSON) > 0 {
		if err := json.Unmarshal(icpJSON, &o.ICP); err != nil {
			return Opportunity{}, fmt.Errorf("opportunity: decode icp for %s: %w", oppID, err)
		}
	}
	claims, err := s.LoadEvidence(ctx, oppID)
	if err != nil {
		return Opportunity{}, err
	}
	o.Claims = claims
	return o, nil
}

// RecordScore persists a computed scorecard bound to its digest and config
// version.
func (s *Store) RecordScore(ctx context.Context, oppID string, sc Scorecard) error {
	id, err := newID("score")
	if err != nil {
		return err
	}
	digest, err := sc.Digest()
	if err != nil {
		return err
	}
	scJSON, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("opportunity: encode scorecard: %w", err)
	}
	const q = `
INSERT INTO opportunity_scores (id, opportunity_id, scorecard, scorecard_digest, config_version)
VALUES ($1,$2,$3,$4,$5)`
	if _, err := s.db.ExecContext(ctx, q, id, oppID, scJSON, digest, sc.ConfigVersion); err != nil {
		return fmt.Errorf("opportunity: record score for %s: %w", oppID, err)
	}
	return nil
}

// RecordVerdict persists a verdict, computing and storing the scorecard and
// thresholds digests so the verdict is bound to exactly what produced it.
func (s *Store) RecordVerdict(ctx context.Context, oppID string, v Verdict, unmet []string, sc Scorecard, t Thresholds) (VerdictRecord, error) {
	id, err := newID("verdict")
	if err != nil {
		return VerdictRecord{}, err
	}
	scDigest, err := sc.Digest()
	if err != nil {
		return VerdictRecord{}, err
	}
	tDigest, err := ThresholdsDigest(t)
	if err != nil {
		return VerdictRecord{}, err
	}
	if unmet == nil {
		unmet = []string{}
	}
	unmetJSON, err := json.Marshal(unmet)
	if err != nil {
		return VerdictRecord{}, fmt.Errorf("opportunity: encode unmet: %w", err)
	}
	scJSON, err := json.Marshal(sc)
	if err != nil {
		return VerdictRecord{}, fmt.Errorf("opportunity: encode scorecard: %w", err)
	}
	tJSON, err := json.Marshal(t)
	if err != nil {
		return VerdictRecord{}, fmt.Errorf("opportunity: encode thresholds: %w", err)
	}
	const q = `
INSERT INTO opportunity_verdicts
  (id, opportunity_id, verdict, unmet_thresholds, scorecard_digest, thresholds_digest,
   config_version, scorecard, thresholds)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING created_at`
	rec := VerdictRecord{
		ID:               id,
		OpportunityID:    oppID,
		Verdict:          v,
		UnmetThresholds:  unmet,
		ScorecardDigest:  scDigest,
		ThresholdsDigest: tDigest,
		ConfigVersion:    sc.ConfigVersion,
		Scorecard:        sc,
		Thresholds:       t,
	}
	if err := s.db.QueryRowContext(ctx, q,
		id, oppID, string(v), unmetJSON, scDigest, tDigest, sc.ConfigVersion, scJSON, tJSON,
	).Scan(&rec.CreatedAt); err != nil {
		return VerdictRecord{}, fmt.Errorf("opportunity: record verdict for %s: %w", oppID, err)
	}
	return rec, nil
}

// LatestVerdict returns the most recent verdict for an opportunity.
func (s *Store) LatestVerdict(ctx context.Context, oppID string) (VerdictRecord, error) {
	const q = `
SELECT id, opportunity_id, verdict, unmet_thresholds, scorecard_digest, thresholds_digest,
       config_version, scorecard, thresholds, created_at
FROM opportunity_verdicts
WHERE opportunity_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1`
	var rec VerdictRecord
	var verdict string
	var unmetJSON, scJSON, tJSON []byte
	err := s.db.QueryRowContext(ctx, q, oppID).Scan(
		&rec.ID, &rec.OpportunityID, &verdict, &unmetJSON, &rec.ScorecardDigest, &rec.ThresholdsDigest,
		&rec.ConfigVersion, &scJSON, &tJSON, &rec.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VerdictRecord{}, ErrNotFound
	}
	if err != nil {
		return VerdictRecord{}, fmt.Errorf("opportunity: latest verdict for %s: %w", oppID, err)
	}
	rec.Verdict = Verdict(verdict)
	if len(unmetJSON) > 0 {
		if err := json.Unmarshal(unmetJSON, &rec.UnmetThresholds); err != nil {
			return VerdictRecord{}, fmt.Errorf("opportunity: decode unmet for %s: %w", oppID, err)
		}
	}
	if len(scJSON) > 0 {
		if err := json.Unmarshal(scJSON, &rec.Scorecard); err != nil {
			return VerdictRecord{}, fmt.Errorf("opportunity: decode scorecard for %s: %w", oppID, err)
		}
	}
	if len(tJSON) > 0 {
		if err := json.Unmarshal(tJSON, &rec.Thresholds); err != nil {
			return VerdictRecord{}, fmt.Errorf("opportunity: decode thresholds for %s: %w", oppID, err)
		}
	}
	return rec, nil
}
