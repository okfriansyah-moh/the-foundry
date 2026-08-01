package kernel

import (
	"context"
	"fmt"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// Result codes for fail-closed SCM provider selection (docs/PLAN.md Task 140).
const (
	ResultSCMProviderMissing  state.ResultCode = "SCM_PROVIDER_MISSING"
	ResultSCMProviderUnknown  state.ResultCode = "SCM_PROVIDER_UNKNOWN"
	ResultSCMProviderMismatch state.ResultCode = "SCM_PROVIDER_MISMATCH"
	ResultSCMWriterMissing    state.ResultCode = "SCM_WRITER_MISSING"
	ResultSCMPolicyAbsent     state.ResultCode = "SCM_POLICY_ABSENT"
)

// SCMProviderName is the closed V1 vocabulary: github | bitbucket.
type SCMProviderName string

const (
	SCMProviderGitHub    SCMProviderName = "github"
	SCMProviderBitbucket SCMProviderName = "bitbucket"
)

// Valid reports whether p is in the closed V1 vocabulary.
func (p SCMProviderName) Valid() bool {
	switch p {
	case SCMProviderGitHub, SCMProviderBitbucket:
		return true
	default:
		return false
	}
}

// BranchPusher is the kernel-internal write seam (one PushBranch per provider).
type BranchPusher interface {
	PushBranch(ctx context.Context, req write.PushRequest) (write.Receipt, error)
}

// SCMWriterRegistry maps allowlisted provider names to writers. Selection is
// kernel-owned; callers/PEC/executors cannot inject a writer.
type SCMWriterRegistry struct {
	writers map[SCMProviderName]BranchPusher
}

// NewSCMWriterRegistry returns an empty registry.
func NewSCMWriterRegistry() *SCMWriterRegistry {
	return &SCMWriterRegistry{writers: map[SCMProviderName]BranchPusher{}}
}

// Register adds a writer for name. Unknown names are rejected.
func (r *SCMWriterRegistry) Register(name SCMProviderName, w BranchPusher) error {
	if !name.Valid() {
		return fmt.Errorf("kernel: unknown SCM provider %q", name)
	}
	if w == nil {
		return fmt.Errorf("kernel: nil writer for %s", name)
	}
	if r.writers == nil {
		r.writers = map[SCMProviderName]BranchPusher{}
	}
	r.writers[name] = w
	return nil
}

// Get returns the writer for name, or false.
func (r *SCMWriterRegistry) Get(name SCMProviderName) (BranchPusher, bool) {
	if r == nil || r.writers == nil {
		return nil, false
	}
	w, ok := r.writers[name]
	return w, ok
}

// SCMProviderSelection is the resolved, policy-derived provider choice.
type SCMProviderSelection struct {
	Provider     SCMProviderName
	PolicyDigest string
}

// SelectSCMProvider resolves the provider from compiled organization policy.
// There is no default-to-GitHub: missing, unknown, or absent policy refuses
// with a named result code before any push (C4/C24).
func SelectSCMProvider(policyProvider string, policyDigest string, remoteURL string, registry *SCMWriterRegistry) (SCMProviderSelection, state.ResultCode, error) {
	if strings.TrimSpace(policyDigest) == "" {
		return SCMProviderSelection{}, ResultSCMPolicyAbsent, fmt.Errorf("kernel: compiled organization policy digest absent")
	}
	name := SCMProviderName(strings.TrimSpace(strings.ToLower(policyProvider)))
	if name == "" {
		return SCMProviderSelection{}, ResultSCMProviderMissing, fmt.Errorf("kernel: scm_provider missing from compiled organization policy")
	}
	if !name.Valid() {
		return SCMProviderSelection{}, ResultSCMProviderUnknown, fmt.Errorf("kernel: unknown scm_provider %q", policyProvider)
	}
	if err := matchRemoteProvider(name, remoteURL); err != nil {
		return SCMProviderSelection{}, ResultSCMProviderMismatch, err
	}
	if registry == nil {
		return SCMProviderSelection{}, ResultSCMWriterMissing, fmt.Errorf("kernel: SCM writer registry not configured")
	}
	if _, ok := registry.Get(name); !ok {
		return SCMProviderSelection{}, ResultSCMWriterMissing, fmt.Errorf("kernel: no writer registered for scm_provider %q", name)
	}
	return SCMProviderSelection{Provider: name, PolicyDigest: policyDigest}, "", nil
}

// matchRemoteProvider refuses when a remote URL host is inconsistent with the
// selected provider. Empty remoteURL skips host matching (local file:// fixtures).
func matchRemoteProvider(name SCMProviderName, remoteURL string) error {
	u := strings.ToLower(remoteURL)
	if u == "" || strings.HasPrefix(u, "file:") {
		return nil
	}
	switch name {
	case SCMProviderGitHub:
		if strings.Contains(u, "bitbucket.org") {
			return fmt.Errorf("kernel: scm_provider github mismatches remote %q", remoteURL)
		}
	case SCMProviderBitbucket:
		if strings.Contains(u, "github.com") {
			return fmt.Errorf("kernel: scm_provider bitbucket mismatches remote %q", remoteURL)
		}
	}
	return nil
}

// PushBranchSelected pushes through the policy-selected writer. Caller-supplied
// provider strings are ignored — only sel.Provider is used.
func PushBranchSelected(
	ctx context.Context,
	registry *SCMWriterRegistry,
	sel SCMProviderSelection,
	req write.PushRequest,
) (write.Receipt, error) {
	w, ok := registry.Get(sel.Provider)
	if !ok {
		return write.Receipt{}, fmt.Errorf("kernel: no writer for %s", sel.Provider)
	}
	return w.PushBranch(ctx, req)
}

// githubBranchPusher adapts write.Pusher to BranchPusher.
type githubBranchPusher struct {
	leases LeaseStore
	ledger ExternalOpStore
	tokens write.TokenSource
}

func (p githubBranchPusher) PushBranch(ctx context.Context, req write.PushRequest) (write.Receipt, error) {
	return PushBranch(ctx, p.leases, p.ledger, p.tokens, req)
}

// bitbucketBranchPusher adapts write.BitbucketPusher to BranchPusher.
type bitbucketBranchPusher struct {
	leases LeaseStore
	ledger ExternalOpStore
	tokens write.TokenSource
}

func (p bitbucketBranchPusher) PushBranch(ctx context.Context, req write.PushRequest) (write.Receipt, error) {
	pusher := &write.BitbucketPusher{
		Leases: leaseAdapter{store: p.leases},
		Ledger: p.ledger,
		Tokens: p.tokens,
		Holder: "kernel",
	}
	return pusher.PushBranch(ctx, req)
}

// NewDefaultSCMWriterRegistry wires GitHub and Bitbucket writers from shared
// lease/ledger/token sources. Provider selection remains SelectSCMProvider's job.
func NewDefaultSCMWriterRegistry(leases LeaseStore, ledger ExternalOpStore, githubTokens, bitbucketTokens write.TokenSource) (*SCMWriterRegistry, error) {
	r := NewSCMWriterRegistry()
	if err := r.Register(SCMProviderGitHub, githubBranchPusher{leases: leases, ledger: ledger, tokens: githubTokens}); err != nil {
		return nil, err
	}
	if err := r.Register(SCMProviderBitbucket, bitbucketBranchPusher{leases: leases, ledger: ledger, tokens: bitbucketTokens}); err != nil {
		return nil, err
	}
	return r, nil
}
