package profile

import (
	"fmt"
	"strings"
)

// docs/PLAN.md Task 149 (SEC-06): RuntimeIsolation trust-domain manifest.

// RuntimeIsolation is resolved at startup for exactly one profile/trust domain.
type RuntimeIsolation struct {
	ProfileID         string
	Kind              Kind
	DBIdentity        string
	TemporalNamespace string
	EvidencePrefix    string
	SecretScope       string
	SCMIdentity       string
	DeployIdentity    string
	BillingIdentity   string
	TelegramScope     string
	AuditChainID      string
	LedgerScope       string
	PortfolioScope    string
}

// ValidateStartup refuses incomplete or multi-profile-unsafe isolation.
func (r RuntimeIsolation) ValidateStartup() error {
	if strings.TrimSpace(r.ProfileID) == "" {
		return fmt.Errorf("profile: RuntimeIsolation.ProfileID required")
	}
	if !r.Kind.Valid() {
		return fmt.Errorf("profile: RuntimeIsolation.Kind invalid")
	}
	required := map[string]string{
		"db_identity":        r.DBIdentity,
		"temporal_namespace": r.TemporalNamespace,
		"evidence_prefix":    r.EvidencePrefix,
		"secret_scope":       r.SecretScope,
		"ledger_scope":       r.LedgerScope,
		"portfolio_scope":    r.PortfolioScope,
		"audit_chain_id":     r.AuditChainID,
	}
	for k, v := range required {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("profile: RuntimeIsolation.%s required (fail-closed)", k)
		}
	}
	if r.EvidencePrefix == "/" || r.EvidencePrefix == "*" {
		return fmt.Errorf("profile: evidence_prefix must be profile-scoped, not global")
	}
	return nil
}

// CompatibleWithEnvelope reports whether an envelope's profile/org matches this
// deployment's trust domain (Task 141 ownership check).
func (r RuntimeIsolation) CompatibleWithEnvelope(profileID, organizationID string) error {
	if profileID != "" && profileID != r.ProfileID {
		return fmt.Errorf("profile: envelope profile %q rejected by runtime isolation %q", profileID, r.ProfileID)
	}
	if r.Kind == Organization {
		if organizationID == "" {
			return fmt.Errorf("profile: organization runtime requires organization_id on envelope")
		}
	}
	if r.Kind == Personal && organizationID != "" {
		return fmt.Errorf("profile: personal runtime rejects organization envelope")
	}
	return nil
}

// RefuseMultiProfileSingleProcess fails closed when more than one profile claims
// to share a process without independent resource identities.
func RefuseMultiProfileSingleProcess(manifests []RuntimeIsolation) error {
	if len(manifests) <= 1 {
		return nil
	}
	seen := map[string]string{}
	fields := func(r RuntimeIsolation) [][2]string {
		return [][2]string{
			{"db", r.DBIdentity},
			{"temporal", r.TemporalNamespace},
			{"evidence", r.EvidencePrefix},
			{"secrets", r.SecretScope},
			{"ledger", r.LedgerScope},
		}
	}
	for _, m := range manifests {
		if err := m.ValidateStartup(); err != nil {
			return err
		}
		for _, kv := range fields(m) {
			key := kv[0] + "=" + kv[1]
			if prev, ok := seen[key]; ok && prev != m.ProfileID {
				return fmt.Errorf("profile: multi-profile single-process shares %s between %s and %s (refused)", kv[0], prev, m.ProfileID)
			}
			seen[key] = m.ProfileID
		}
	}
	return nil
}
