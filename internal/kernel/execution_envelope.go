package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// docs/PLAN.md Task 141 (RTC-05): one versioned, immutable, canonical,
// digestible execution envelope carrying every authority-bearing decision
// required by a delivery. Only the kernel resolves or widens authority
// (Constitution C4, C7, C19, C24).

const (
	// ExecutionEnvelopeSchemaV1 is the only schema version this build accepts.
	ExecutionEnvelopeSchemaV1 = "1"
)

// Closed vocabularies for envelope fields. Unknown values refuse resolution.
const (
	ProviderGitHub    = "github"
	ProviderBitbucket = "bitbucket"
	ProviderLocal     = "local"

	WorkspaceStrategyIsolated = "isolated-worktree"

	BudgetScopeMission  = "mission"
	BudgetScopeWorkflow = "workflow"
	BudgetScopeSession  = "session"

	DeploymentModeAuto     = "auto"
	DeploymentModeCommand  = "command"
	DeploymentModeDisabled = "disabled"

	BranchDeliveryPolicyNone   = "none"
	BranchDeliveryPolicyTenX   = "ten-x-handoff"
	BranchDeliveryPolicyDirect = "direct"
)

var (
	// ErrEnvelopeRefused is returned when authority cannot be resolved or
	// verification fails (missing source, expiry, tamper, widening).
	ErrEnvelopeRefused = errors.New("kernel: execution envelope refused")
	// ErrEnvelopeNotFound is returned by EnvelopeStore.Load when the id/digest
	// has no row.
	ErrEnvelopeNotFound = errors.New("kernel: execution envelope not found")
	// ErrEnvelopeTampered is returned when a stored digest does not match the
	// recomputed canonical digest.
	ErrEnvelopeTampered = errors.New("kernel: execution envelope tampered")
	// ErrEnvelopeExpired is returned when validity.expires_at has passed.
	ErrEnvelopeExpired = errors.New("kernel: execution envelope expired")
	// ErrEnvelopeRevoked is returned when the stored envelope was revoked.
	ErrEnvelopeRevoked = errors.New("kernel: execution envelope revoked")
	// ErrEnvelopeSchema is returned for unknown schema_version values.
	ErrEnvelopeSchema = errors.New("kernel: unknown execution envelope schema")
)

// ExecutionEnvelope is the canonical authority record for one delivery.
// Fields match docs/PLAN.md Task 141 schema exactly. Digests are computed
// over CanonicalJSON with EnvelopeDigest empty.
type ExecutionEnvelope struct {
	SchemaVersion  string                 `json:"schema_version"`
	EnvelopeID     string                 `json:"envelope_id"`
	EnvelopeDigest string                 `json:"envelope_digest"`
	Plan           EnvelopePlan           `json:"plan"`
	Repository     EnvelopeRepository     `json:"repository"`
	Ownership      EnvelopeOwnership      `json:"ownership"`
	Execution      EnvelopeExecution      `json:"execution"`
	Cost           EnvelopeCost           `json:"cost"`
	Policy         EnvelopePolicy         `json:"policy"`
	Validity       EnvelopeValidity       `json:"validity"`
}

// EnvelopePlan binds the ApprovedPlan artifact and approval evidence.
type EnvelopePlan struct {
	ApprovedPlanID       string    `json:"approved_plan_id"`
	PlanDigest           string    `json:"plan_digest"`
	PlanArtifactRef      string    `json:"plan_artifact_ref"`
	ApprovalRef          string    `json:"approval_ref"`
	ApprovalSignatureRef string    `json:"approval_signature_ref"`
	ApprovalExpiresAt    time.Time `json:"approval_expires_at"`
}

// EnvelopeRepository names the immutable repository context.
type EnvelopeRepository struct {
	RepositoryID         string `json:"repository_id"`
	Provider             string `json:"provider"`
	CanonicalURL         string `json:"canonical_url"`
	RepositoryAlias      string `json:"repository_alias"`
	PinnedBaseRevision   string `json:"pinned_base_revision"`
	RequestedTargetBranch string `json:"requested_target_branch"`
	WorkspaceStrategy    string `json:"workspace_strategy"`
}

// EnvelopeOwnership attributes the delivery to mission/profile principals.
type EnvelopeOwnership struct {
	MissionID      string `json:"mission_id"`
	PortfolioID    string `json:"portfolio_id"`
	ProfileID      string `json:"profile_id"`
	OrganizationID string `json:"organization_id"`
	PrincipalID    string `json:"principal_id"`
}

// EnvelopeExecution carries sandbox/executor/validation allowlists and effect
// policy resolved from compiled layers — never from transport.
type EnvelopeExecution struct {
	Unattended            bool     `json:"unattended"`
	RequireSandbox        bool     `json:"require_sandbox"`
	ExecutorAllowlist     []string `json:"executor_allowlist"`
	ValidationAllowlistRef string  `json:"validation_allowlist_ref"`
	MaxWaveConcurrency    int      `json:"max_wave_concurrency"`
	PermittedEffects      []string `json:"permitted_effects"`
	DeploymentMode        string   `json:"deployment_mode"`
	BranchDeliveryPolicy  string   `json:"branch_delivery_policy"`
}

// EnvelopeCost scopes budget attribution for the delivery.
type EnvelopeCost struct {
	BudgetScope       string  `json:"budget_scope"`
	BudgetScopeID     string  `json:"budget_scope_id"`
	BudgetEnvelopeID  string  `json:"budget_envelope_id"`
	SessionCapUSD     float64 `json:"session_cap_usd"`
	ExperimentCapUSD  float64 `json:"experiment_cap_usd"`
	DeploymentCapUSD  float64 `json:"deployment_cap_usd"`
}

// EnvelopePolicy records the compiled policy identity that authorized this
// envelope.
type EnvelopePolicy struct {
	PolicyDigest            string   `json:"policy_digest"`
	PolicyVersion           string   `json:"policy_version"`
	ResolvedLayerDigests    []string `json:"resolved_layer_digests"`
	AuthorizationDecisionRef string  `json:"authorization_decision_ref"`
}

// EnvelopeValidity bounds when the envelope may be used to start or continue.
type EnvelopeValidity struct {
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// ResolveExecutionEnvelopeInput is the kernel-internal intent for resolution.
// Transports may supply references (plan id, mission id, profile id) but never
// executor/queue/sandbox/budget/policy authority fields.
type ResolveExecutionEnvelopeInput struct {
	PlanID            string
	PlanArtifactRef   string
	RepositoryID      string
	Provider          string
	CanonicalURL      string
	RepositoryAlias   string
	PinnedBaseRevision string
	TargetBranch      string
	MissionID         string
	PortfolioID       string
	ProfileID         string
	OrganizationID    string
	PrincipalID       string
	Unattended        bool
	// RequireSandboxOverride is only honored when false would NOT widen a
	// profile that requires sandbox (C24). Prefer leaving it nil.
	RequireSandbox *bool
	MaxWaveConcurrency int
	BudgetScope       string
	BudgetScopeID     string
	BudgetEnvelopeID  string
	SessionCapUSD     float64
	ExperimentCapUSD  float64
	DeploymentCapUSD  float64
	DeploymentMode    string
	BranchDeliveryPolicy string
	PermittedEffects  []string
	AuthorizationDecisionRef string
	IssuedAt          time.Time
	ExpiresAt         *time.Time
	EnvelopeID        string // optional; generated when empty
}

// EnvelopeResolverDeps are the authoritative sources ResolveExecutionEnvelope
// reads. Absence of a required source refuses (C24).
type EnvelopeResolverDeps struct {
	Provenance *provenance.Store
	// Policy is the already-compiled effective policy for the target profile.
	Policy *compiler.Resolved
	// LayerDigests are the per-layer digests that produced Policy.
	LayerDigests []string
	PolicyVersion string
	Now          func() time.Time
}

// ResolveExecutionEnvelope builds a canonical envelope from authoritative
// records. It never trusts transport-supplied executor/policy/budget/sandbox
// widenings.
func ResolveExecutionEnvelope(ctx context.Context, deps EnvelopeResolverDeps, in ResolveExecutionEnvelopeInput) (*ExecutionEnvelope, error) {
	if deps.Provenance == nil {
		return nil, fmt.Errorf("%w: missing provenance store", ErrEnvelopeRefused)
	}
	if deps.Policy == nil {
		return nil, fmt.Errorf("%w: missing policy digest", ErrEnvelopeRefused)
	}
	if deps.Policy.Digest == "" {
		return nil, fmt.Errorf("%w: missing policy digest", ErrEnvelopeRefused)
	}
	if len(deps.Policy.Effective.ExecutorAllowlist) == 0 {
		return nil, fmt.Errorf("%w: missing executor allowlist", ErrEnvelopeRefused)
	}
	if deps.Policy.Effective.ValidationAllowlistRef == "" {
		return nil, fmt.Errorf("%w: missing validation policy", ErrEnvelopeRefused)
	}
	if in.PlanID == "" {
		return nil, fmt.Errorf("%w: missing ApprovedPlan", ErrEnvelopeRefused)
	}

	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	issuedAt := in.IssuedAt.UTC()
	if issuedAt.IsZero() {
		issuedAt = now
	}

	approved, err := deps.Provenance.Load(ctx, in.PlanID)
	if err != nil {
		if errors.Is(err, provenance.ErrPlanRevoked) || errors.Is(err, provenance.ErrPlanExpired) || errors.Is(err, provenance.ErrNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrEnvelopeRefused, err)
		}
		return nil, fmt.Errorf("kernel: load ApprovedPlan for envelope: %w", err)
	}
	if approved.PlanDigest() == "" {
		return nil, fmt.Errorf("%w: missing plan digest", ErrEnvelopeRefused)
	}
	artifactRef := in.PlanArtifactRef
	if artifactRef == "" {
		artifactRef = "approved-plan:" + in.PlanID
	}

	if in.PinnedBaseRevision == "" && in.RepositoryID == "" && in.CanonicalURL == "" {
		// Repository context may be deferred to Task 143 registry resolution,
		// but unattended deliveries still require a pinned revision once a
		// repository is named. When no repository is named at all, refuse for
		// unattended (C24) — attended local-dev paths may still resolve later.
		if in.Unattended {
			return nil, fmt.Errorf("%w: missing repository revision", ErrEnvelopeRefused)
		}
	}
	if (in.RepositoryID != "" || in.CanonicalURL != "") && in.PinnedBaseRevision == "" {
		return nil, fmt.Errorf("%w: missing repository revision", ErrEnvelopeRefused)
	}

	provider := in.Provider
	if provider == "" {
		provider = ProviderLocal
	}
	if !validProvider(provider) {
		return nil, fmt.Errorf("%w: unknown repository provider %q", ErrEnvelopeRefused, provider)
	}

	requireSandbox := profileRequiresSandbox(in.ProfileID, deps.Policy)
	if in.RequireSandbox != nil {
		if !*in.RequireSandbox && requireSandbox {
			return nil, fmt.Errorf("%w: sandbox-required profile cannot set require_sandbox=false", ErrEnvelopeRefused)
		}
		if *in.RequireSandbox {
			requireSandbox = true
		}
	}
	// Autonomous personal/org profiles always require sandbox when unattended.
	if in.Unattended {
		requireSandbox = true
	}

	if in.OrganizationID != "" && in.ProfileID == "" {
		return nil, fmt.Errorf("%w: organization/profile mismatch", ErrEnvelopeRefused)
	}
	if err := checkOwnershipConsistency(in, approved); err != nil {
		return nil, err
	}

	budgetScope := in.BudgetScope
	if budgetScope == "" {
		if in.MissionID != "" {
			budgetScope = BudgetScopeMission
		} else {
			budgetScope = BudgetScopeWorkflow
		}
	}
	if !validBudgetScope(budgetScope) {
		return nil, fmt.Errorf("%w: unknown budget scope %q", ErrEnvelopeRefused, budgetScope)
	}
	budgetScopeID := in.BudgetScopeID
	if budgetScopeID == "" {
		switch budgetScope {
		case BudgetScopeMission:
			budgetScopeID = in.MissionID
		default:
			budgetScopeID = in.PlanID
		}
	}
	if in.Unattended && budgetScopeID == "" {
		return nil, fmt.Errorf("%w: missing budget scope id for unattended delivery", ErrEnvelopeRefused)
	}

	deploymentMode := in.DeploymentMode
	if deploymentMode == "" {
		deploymentMode = string(deps.Policy.Effective.DeploymentModes["production"])
		if deploymentMode == "" {
			deploymentMode = DeploymentModeCommand
		}
	}
	if !validDeploymentMode(deploymentMode) {
		return nil, fmt.Errorf("%w: unknown deployment mode %q", ErrEnvelopeRefused, deploymentMode)
	}

	branchPolicy := in.BranchDeliveryPolicy
	if branchPolicy == "" {
		branchPolicy = BranchDeliveryPolicyNone
	}
	if !validBranchPolicy(branchPolicy) {
		return nil, fmt.Errorf("%w: unknown branch delivery policy %q", ErrEnvelopeRefused, branchPolicy)
	}

	workspaceStrategy := WorkspaceStrategyIsolated
	maxWave := in.MaxWaveConcurrency
	if maxWave < 0 {
		return nil, fmt.Errorf("%w: max_wave_concurrency cannot be negative", ErrEnvelopeRefused)
	}

	execAllow := append([]string(nil), deps.Policy.Effective.ExecutorAllowlist...)
	sort.Strings(execAllow)
	effects := append([]string(nil), in.PermittedEffects...)
	sort.Strings(effects)
	layerDigests := append([]string(nil), deps.LayerDigests...)
	sort.Strings(layerDigests)

	policyVersion := deps.PolicyVersion
	if policyVersion == "" {
		policyVersion = "1"
	}

	envelopeID := in.EnvelopeID
	if envelopeID == "" {
		envelopeID = newEnvelopeID(approved.PlanDigest(), issuedAt)
	}

	var approvalSigRef string
	if sig := approved.Signature(); len(sig) > 0 {
		sum := sha256.Sum256(sig)
		approvalSigRef = "sha256:" + hex.EncodeToString(sum[:])
	}

	env := &ExecutionEnvelope{
		SchemaVersion: ExecutionEnvelopeSchemaV1,
		EnvelopeID:    envelopeID,
		Plan: EnvelopePlan{
			ApprovedPlanID:       in.PlanID,
			PlanDigest:           approved.PlanDigest(),
			PlanArtifactRef:      artifactRef,
			ApprovalRef:          "approved-plan:" + in.PlanID,
			ApprovalSignatureRef: approvalSigRef,
			ApprovalExpiresAt:    approved.ExpiresAt().UTC(),
		},
		Repository: EnvelopeRepository{
			RepositoryID:          in.RepositoryID,
			Provider:              provider,
			CanonicalURL:          in.CanonicalURL,
			RepositoryAlias:       in.RepositoryAlias,
			PinnedBaseRevision:    in.PinnedBaseRevision,
			RequestedTargetBranch: in.TargetBranch,
			WorkspaceStrategy:     workspaceStrategy,
		},
		Ownership: EnvelopeOwnership{
			MissionID:      in.MissionID,
			PortfolioID:    in.PortfolioID,
			ProfileID:      in.ProfileID,
			OrganizationID: in.OrganizationID,
			PrincipalID:    in.PrincipalID,
		},
		Execution: EnvelopeExecution{
			Unattended:             in.Unattended,
			RequireSandbox:         requireSandbox,
			ExecutorAllowlist:      execAllow,
			ValidationAllowlistRef: deps.Policy.Effective.ValidationAllowlistRef,
			MaxWaveConcurrency:     maxWave,
			PermittedEffects:       effects,
			DeploymentMode:         deploymentMode,
			BranchDeliveryPolicy:   branchPolicy,
		},
		Cost: EnvelopeCost{
			BudgetScope:      budgetScope,
			BudgetScopeID:    budgetScopeID,
			BudgetEnvelopeID: in.BudgetEnvelopeID,
			SessionCapUSD:    in.SessionCapUSD,
			ExperimentCapUSD: in.ExperimentCapUSD,
			DeploymentCapUSD: in.DeploymentCapUSD,
		},
		Policy: EnvelopePolicy{
			PolicyDigest:             deps.Policy.Digest,
			PolicyVersion:            policyVersion,
			ResolvedLayerDigests:     layerDigests,
			AuthorizationDecisionRef: in.AuthorizationDecisionRef,
		},
		Validity: EnvelopeValidity{
			IssuedAt:  issuedAt,
			ExpiresAt: cloneTimePtr(in.ExpiresAt),
		},
	}

	digest, err := env.ComputeDigest()
	if err != nil {
		return nil, fmt.Errorf("kernel: compute envelope digest: %w", err)
	}
	env.EnvelopeDigest = digest
	if err := env.Validate(); err != nil {
		return nil, err
	}
	return env, nil
}

// ComputeDigest returns the sha256 hex digest of the envelope's canonical JSON
// with EnvelopeDigest cleared.
func (e ExecutionEnvelope) ComputeDigest() (string, error) {
	clone := e
	clone.EnvelopeDigest = ""
	raw, err := clone.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON returns deterministic compact JSON for digesting and storage.
func (e ExecutionEnvelope) CanonicalJSON() ([]byte, error) {
	if e.SchemaVersion == "" {
		return nil, fmt.Errorf("%w: empty schema_version", ErrEnvelopeSchema)
	}
	// Normalize slices for stability even if callers forgot to sort.
	e.Execution.ExecutorAllowlist = sortedCopy(e.Execution.ExecutorAllowlist)
	e.Execution.PermittedEffects = sortedCopy(e.Execution.PermittedEffects)
	e.Policy.ResolvedLayerDigests = sortedCopy(e.Policy.ResolvedLayerDigests)
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("kernel: marshal execution envelope: %w", err)
	}
	return raw, nil
}

// Validate checks closed vocabularies, schema version, and digest integrity.
func (e *ExecutionEnvelope) Validate() error {
	if e == nil {
		return fmt.Errorf("%w: nil envelope", ErrEnvelopeRefused)
	}
	if e.SchemaVersion != ExecutionEnvelopeSchemaV1 {
		return fmt.Errorf("%w: %q", ErrEnvelopeSchema, e.SchemaVersion)
	}
	if e.EnvelopeID == "" || e.EnvelopeDigest == "" {
		return fmt.Errorf("%w: missing envelope id/digest", ErrEnvelopeRefused)
	}
	got, err := e.ComputeDigest()
	if err != nil {
		return err
	}
	if got != e.EnvelopeDigest {
		return fmt.Errorf("%w: digest mismatch", ErrEnvelopeTampered)
	}
	if e.Plan.ApprovedPlanID == "" || e.Plan.PlanDigest == "" {
		return fmt.Errorf("%w: missing ApprovedPlan", ErrEnvelopeRefused)
	}
	if e.Plan.PlanArtifactRef == "" {
		return fmt.Errorf("%w: missing plan artifact", ErrEnvelopeRefused)
	}
	if len(e.Execution.ExecutorAllowlist) == 0 {
		return fmt.Errorf("%w: missing executor allowlist", ErrEnvelopeRefused)
	}
	if e.Execution.ValidationAllowlistRef == "" {
		return fmt.Errorf("%w: missing validation policy", ErrEnvelopeRefused)
	}
	if e.Policy.PolicyDigest == "" {
		return fmt.Errorf("%w: missing policy digest", ErrEnvelopeRefused)
	}
	if !validProvider(e.Repository.Provider) {
		return fmt.Errorf("%w: unknown repository provider %q", ErrEnvelopeRefused, e.Repository.Provider)
	}
	if e.Repository.WorkspaceStrategy != "" && e.Repository.WorkspaceStrategy != WorkspaceStrategyIsolated {
		return fmt.Errorf("%w: unknown workspace strategy %q", ErrEnvelopeRefused, e.Repository.WorkspaceStrategy)
	}
	if !validBudgetScope(e.Cost.BudgetScope) {
		return fmt.Errorf("%w: unknown budget scope %q", ErrEnvelopeRefused, e.Cost.BudgetScope)
	}
	if !validDeploymentMode(e.Execution.DeploymentMode) {
		return fmt.Errorf("%w: unknown deployment mode %q", ErrEnvelopeRefused, e.Execution.DeploymentMode)
	}
	if !validBranchPolicy(e.Execution.BranchDeliveryPolicy) {
		return fmt.Errorf("%w: unknown branch delivery policy %q", ErrEnvelopeRefused, e.Execution.BranchDeliveryPolicy)
	}
	return nil
}

// VerifyUsable re-validates digest integrity and refuses expired/revoked use.
func (e *ExecutionEnvelope) VerifyUsable(now time.Time, revoked bool) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if revoked {
		return fmt.Errorf("%w", ErrEnvelopeRevoked)
	}
	if e.Validity.ExpiresAt != nil && !e.Validity.ExpiresAt.IsZero() && now.After(e.Validity.ExpiresAt.UTC()) {
		return fmt.Errorf("%w", ErrEnvelopeExpired)
	}
	return nil
}

// RefuseTransportWidening compares a transport-supplied intent against the
// stored envelope and refuses any attempt to widen authority fields.
func (e *ExecutionEnvelope) RefuseTransportWidening(in ResolveExecutionEnvelopeInput) error {
	if in.Unattended != e.Execution.Unattended && in.Unattended && !e.Execution.Unattended {
		// Attended→unattended is a widening of autonomy.
		return fmt.Errorf("%w: transport cannot widen unattended", ErrEnvelopeRefused)
	}
	if in.RequireSandbox != nil && !*in.RequireSandbox && e.Execution.RequireSandbox {
		return fmt.Errorf("%w: transport cannot widen require_sandbox", ErrEnvelopeRefused)
	}
	if in.MaxWaveConcurrency > 0 && e.Execution.MaxWaveConcurrency > 0 && in.MaxWaveConcurrency > e.Execution.MaxWaveConcurrency {
		return fmt.Errorf("%w: transport cannot widen max_wave_concurrency", ErrEnvelopeRefused)
	}
	if in.SessionCapUSD > 0 && e.Cost.SessionCapUSD > 0 && in.SessionCapUSD > e.Cost.SessionCapUSD {
		return fmt.Errorf("%w: transport cannot widen session_cap_usd", ErrEnvelopeRefused)
	}
	if len(in.PermittedEffects) > 0 {
		allowed := map[string]bool{}
		for _, p := range e.Execution.PermittedEffects {
			allowed[p] = true
		}
		for _, p := range in.PermittedEffects {
			if !allowed[p] && len(allowed) > 0 {
				return fmt.Errorf("%w: transport cannot widen permitted_effects", ErrEnvelopeRefused)
			}
		}
	}
	return nil
}

func checkOwnershipConsistency(in ResolveExecutionEnvelopeInput, approved *provenance.ApprovedPlan) error {
	// Profile kind on the ApprovedPlan must not contradict organization binding.
	kind := approved.ProfileKind()
	if kind == "organization" && in.OrganizationID == "" && in.ProfileID != "" {
		return fmt.Errorf("%w: organization/profile mismatch", ErrEnvelopeRefused)
	}
	if kind == "personal" && in.OrganizationID != "" {
		return fmt.Errorf("%w: organization/profile mismatch", ErrEnvelopeRefused)
	}
	return nil
}

func profileRequiresSandbox(profileID string, policy *compiler.Resolved) bool {
	if policy != nil && compiler.RequireSandbox(policy.Effective) {
		return true
	}
	switch profileID {
	case "personal-autonomous-venture", "organization-10x":
		return true
	default:
		return false
	}
}

func newEnvelopeID(planDigest string, issuedAt time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("envelope|%s|%d", planDigest, issuedAt.UnixNano())))
	return "env-" + hex.EncodeToString(sum[:])[:32]
}

func sortedCopy(in []string) []string {
	if in == nil {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func validProvider(p string) bool {
	switch p {
	case ProviderGitHub, ProviderBitbucket, ProviderLocal:
		return true
	default:
		return false
	}
}

func validBudgetScope(s string) bool {
	switch s {
	case BudgetScopeMission, BudgetScopeWorkflow, BudgetScopeSession:
		return true
	default:
		return false
	}
}

func validDeploymentMode(m string) bool {
	switch m {
	case DeploymentModeAuto, DeploymentModeCommand, DeploymentModeDisabled:
		return true
	default:
		return false
	}
}

func validBranchPolicy(p string) bool {
	switch p {
	case BranchDeliveryPolicyNone, BranchDeliveryPolicyTenX, BranchDeliveryPolicyDirect:
		return true
	default:
		return false
	}
}
