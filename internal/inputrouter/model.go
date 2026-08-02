package inputrouter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Kind / Origin / Mode are closed vocabularies.
type Kind string

const (
	KindIdea   Kind = "IDEA"
	KindPlan   Kind = "PLAN"
	KindMockup Kind = "MOCKUP"
)

type Origin string

const (
	OriginCLI      Origin = "CLI"
	OriginTelegram Origin = "TELEGRAM"
	OriginAPI      Origin = "API"
)

type ProfileMode string

const (
	ModePersonal     ProfileMode = "personal"
	ModeOrganization ProfileMode = "organization"
)

// InputRequest is the normalized intent-only contract.
type InputRequest struct {
	RequestID      string
	IdempotencyKey string
	Kind           Kind
	Origin         Origin
	PrincipalID    string
	ProfileID      string
	OrganizationID string
	Mode           ProfileMode
	Text           string
	ArtifactRefs   []string // ordered
	PlanRef        string
	BudgetUSD      float64
	SubmittedAt    time.Time
	ClientMeta     map[string]string
}

// RouteDecision is immutable route/reference data — never authority.
type RouteDecision struct {
	Route         string // e.g. personal.intake, personal.deliver, org.tenx
	RequestDigest string
	BundleDigest  string
	RefuseReason  string
}

// ArtifactBundleDigest is the canonical ordered digest of artifact refs.
func ArtifactBundleDigest(refs []string) string {
	h := sha256.New()
	for i, r := range refs {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(r))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DecideRoute deterministically selects the application path from authenticated
// input + profile mode. It never grants authority.
func DecideRoute(in InputRequest) (RouteDecision, error) {
	if err := validateRequest(in); err != nil {
		return RouteDecision{}, err
	}
	bundle := ArtifactBundleDigest(in.ArtifactRefs)
	reqDigest := requestDigest(in, bundle)
	out := RouteDecision{RequestDigest: reqDigest, BundleDigest: bundle}

	switch in.Mode {
	case ModePersonal:
		switch in.Kind {
		case KindIdea:
			out.Route = "personal.intake"
		case KindMockup:
			out.Route = "personal.mockup_intake"
		case KindPlan:
			if strings.EqualFold(in.ClientMeta["mission_contract"], "true") {
				out.Route = "personal.mission_from_plan"
			} else {
				out.Route = "personal.deliver_plan"
			}
		}
	case ModeOrganization:
		switch in.Kind {
		case KindIdea:
			if in.ClientMeta["governed_org_idea"] != "true" {
				out.Route = ""
				out.RefuseReason = "organization IDEA refused unless governed policy route configured"
				return out, nil
			}
			out.Route = "organization.governed_idea"
		case KindMockup:
			out.Route = "organization.mockup_to_tenx"
		case KindPlan:
			out.Route = "organization.tenx"
		}
	default:
		return RouteDecision{}, fmt.Errorf("inputrouter: unknown mode %q", in.Mode)
	}

	// Transport cannot select executor/authority via metadata.
	if _, ok := in.ClientMeta["executor"]; ok {
		return RouteDecision{}, fmt.Errorf("inputrouter: transport cannot select executor")
	}
	if _, ok := in.ClientMeta["authority"]; ok {
		return RouteDecision{}, fmt.Errorf("inputrouter: transport cannot select authority")
	}
	return out, nil
}

func validateRequest(in InputRequest) error {
	if in.RequestID == "" || in.PrincipalID == "" {
		return fmt.Errorf("inputrouter: request_id and principal_id required")
	}
	switch in.Kind {
	case KindIdea, KindPlan, KindMockup:
	default:
		return fmt.Errorf("inputrouter: invalid kind %q", in.Kind)
	}
	switch in.Origin {
	case OriginCLI, OriginTelegram, OriginAPI:
	default:
		return fmt.Errorf("inputrouter: invalid origin %q", in.Origin)
	}
	switch in.Mode {
	case ModePersonal, ModeOrganization:
	default:
		return fmt.Errorf("inputrouter: invalid mode %q", in.Mode)
	}
	if in.Mode == ModeOrganization && in.OrganizationID == "" {
		return fmt.Errorf("inputrouter: organization mode requires organization_id bound to principal")
	}
	return nil
}

func requestDigest(in InputRequest, bundle string) string {
	h := sha256.New()
	parts := []string{in.RequestID, string(in.Kind), string(in.Origin), in.PrincipalID, in.ProfileID, in.OrganizationID, string(in.Mode), in.Text, in.PlanRef, bundle}
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
