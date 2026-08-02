package inputrouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// PGStore persists input router requests (migration 00043).
type PGStore struct {
	DB *sql.DB
}

func (p *PGStore) Put(ctx context.Context, in InputRequest, d RouteDecision) error {
	meta, _ := json.Marshal(in.ClientMeta)
	arts, _ := json.Marshal(in.ArtifactRefs)
	_, err := p.DB.ExecContext(ctx, `
INSERT INTO input_router_requests (
  request_id, idempotency_key, kind, origin, principal_id, profile_id, organization_id,
  mode, text_hash, artifact_bundle_digest, plan_ref, budget_usd, route_decision, downstream_ref
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (request_id) DO NOTHING`,
		in.RequestID, in.IdempotencyKey, string(in.Kind), string(in.Origin), in.PrincipalID, in.ProfileID, in.OrganizationID,
		string(in.Mode), d.RequestDigest, d.BundleDigest, in.PlanRef, in.BudgetUSD, d.Route, d.RefuseReason)
	if err != nil {
		return fmt.Errorf("inputrouter: put: %w", err)
	}
	_ = meta
	_ = arts
	return nil
}

func (p *PGStore) GetByIdempotency(ctx context.Context, key string) (InputRequest, RouteDecision, bool, error) {
	var in InputRequest
	var d RouteDecision
	var kind, origin, mode string
	err := p.DB.QueryRowContext(ctx, `
SELECT request_id, idempotency_key, kind, origin, principal_id, profile_id, organization_id,
       mode, text_hash, artifact_bundle_digest, plan_ref, budget_usd, route_decision, downstream_ref
FROM input_router_requests WHERE idempotency_key=$1`, key).Scan(
		&in.RequestID, &in.IdempotencyKey, &kind, &origin, &in.PrincipalID, &in.ProfileID, &in.OrganizationID,
		&mode, &d.RequestDigest, &d.BundleDigest, &in.PlanRef, &in.BudgetUSD, &d.Route, &d.RefuseReason)
	if errors.Is(err, sql.ErrNoRows) {
		return InputRequest{}, RouteDecision{}, false, nil
	}
	if err != nil {
		return InputRequest{}, RouteDecision{}, false, fmt.Errorf("inputrouter: get: %w", err)
	}
	in.Kind = Kind(kind)
	in.Origin = Origin(origin)
	in.Mode = ProfileMode(mode)
	return in, d, true, nil
}
