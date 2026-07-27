package pdp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/open-policy-agent/opa/rego"

	"github.com/okfriansyah-moh/the-foundry/internal/policy"
	"github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
)

// query is the single rego query this package evaluates for every
// request: it binds both the boolean decision (x) and its human-readable
// explanation (y) from data.foundry.authz in one Eval call.
const query = "x = data.foundry.authz.allow; y = data.foundry.authz.reason"

// OPADecider is the sole implementation of policy.Decider. It embeds OPA
// as a library and evaluates every request against one
// compiler.Resolved policy and one pinned rego bundle digest, both fixed
// at construction time — nothing on the Decide hot path re-reads the
// filesystem, re-merges configuration layers, or otherwise varies what a
// given request can produce.
type OPADecider struct {
	query        rego.PreparedEvalQuery
	bundleDir    string
	bundleDigest string
	resolved     *compiler.Resolved
	policyDoc    map[string]any
}

var _ policy.Decider = (*OPADecider)(nil)

// NewOPADecider loads the rego bundle in bundleDir, refuses to construct a
// Decider if its digest does not match pinnedBundleDigest (the anti-drift
// mechanism this task requires), compiles the bundle once, and binds the
// result to resolved — internal/policy/compiler's already-validated,
// non-weakened output. resolved must be non-nil with a non-empty Digest;
// this constructor has no parameter through which a raw LayerPolicy (or
// any other pre-merge input) could reach the PDP.
func NewOPADecider(ctx context.Context, bundleDir, pinnedBundleDigest string, resolved *compiler.Resolved) (*OPADecider, error) {
	if resolved == nil {
		return nil, fmt.Errorf("pdp: resolved policy is required")
	}
	if resolved.Digest == "" {
		return nil, fmt.Errorf("pdp: resolved policy has no digest")
	}

	files, err := loadRegoFiles(bundleDir)
	if err != nil {
		return nil, err
	}
	actualDigest := digestFiles(files)
	if actualDigest != pinnedBundleDigest {
		return nil, fmt.Errorf("pdp: rego bundle digest mismatch: pinned %s, loaded %s: refusing to evaluate", pinnedBundleDigest, actualDigest)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	opts := []func(*rego.Rego){rego.Query(query)}
	for _, name := range names {
		opts = append(opts, rego.Module(name, files[name]))
	}

	prepared, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("pdp: compile rego bundle: %w", err)
	}

	raw, err := json.Marshal(resolved.Effective)
	if err != nil {
		return nil, fmt.Errorf("pdp: marshal resolved policy: %w", err)
	}
	var policyDoc map[string]any
	if err := json.Unmarshal(raw, &policyDoc); err != nil {
		return nil, fmt.Errorf("pdp: unmarshal resolved policy: %w", err)
	}

	return &OPADecider{
		query:        prepared,
		bundleDir:    bundleDir,
		bundleDigest: actualDigest,
		resolved:     resolved,
		policyDoc:    policyDoc,
	}, nil
}

// Decide answers one runtime authorization question. It is a pure
// function of (req, the compiler.Resolved digest this Decider was
// constructed with): calling it twice with an identical req yields a
// byte-identical Decision, and OPADecider exposes no mutable field that
// could change that output between calls.
//
// If req.PolicyDigest does not match the digest of the Resolved policy
// this Decider was booted with, Decide denies without evaluating the
// bundle at all — a caller holding a stale or different ResolvedPolicy
// can never receive an authorization answer computed against the wrong
// policy generation.
func (d *OPADecider) Decide(ctx context.Context, req policy.Request) (policy.Decision, error) {
	if req.PolicyDigest != d.resolved.Digest {
		return policy.Decision{
			Allow: false,
			Reason: fmt.Sprintf(
				"denied: request policy_digest %q does not match the ResolvedPolicy this PDP was booted with (%q)",
				req.PolicyDigest, d.resolved.Digest,
			),
		}, nil
	}

	reqContext := req.Context
	if reqContext == nil {
		reqContext = map[string]any{}
	}

	input := map[string]any{
		"principal":     req.Principal,
		"action":        req.Action,
		"resource":      req.Resource,
		"context":       reqContext,
		"policy_digest": req.PolicyDigest,
		"policy":        d.policyDoc,
	}

	rs, err := d.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return policy.Decision{}, fmt.Errorf("pdp: evaluate: %w", err)
	}
	if len(rs) == 0 {
		return policy.Decision{}, fmt.Errorf("pdp: evaluate: empty result set")
	}

	allow, _ := rs[0].Bindings["x"].(bool)
	reason, _ := rs[0].Bindings["y"].(string)
	return policy.Decision{Allow: allow, Reason: reason}, nil
}

// VerifyIntegrity re-reads bundleDir from disk and reports an error if its
// digest no longer matches the digest this Decider was booted with. It is
// the detection half of this package's tamper-evidence requirement: Decide
// itself never re-reads disk, so a rego file edited after boot cannot
// silently change a decision; VerifyIntegrity is how an operator or health
// check confirms the on-disk bundle is still the one that was pinned.
func (d *OPADecider) VerifyIntegrity() error {
	files, err := loadRegoFiles(d.bundleDir)
	if err != nil {
		return err
	}
	current := digestFiles(files)
	if current != d.bundleDigest {
		return fmt.Errorf("pdp: rego bundle at %q has changed since boot: pinned %s, now %s", d.bundleDir, d.bundleDigest, current)
	}
	return nil
}
