package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// CrossRepoContract is one declared interface contract shared across repos in
// a change-set. Its Digest is frozen at saga start so every per-repo child
// integrates against the same interface snapshot.
type CrossRepoContract struct {
	Name   string
	Digest string
}

// RepoNode is one repository in a change-set and the repos it depends on
// (integration must respect this ordering).
type RepoNode struct {
	Alias     string
	DependsOn []string
}

// ChangeSetPlan describes one multi-repository 10x initiative (docs/PLAN.md
// Task 78 / EVO-05).
type ChangeSetPlan struct {
	InitiativeID string
	Contracts    []CrossRepoContract
	Repos        []RepoNode
	// EnvRevision is the environment/toolchain revision the whole change-set
	// was produced against — recorded as provenance on the result.
	EnvRevision string
}

// RepoAttempt is the outcome of one repo's TenXDeliver child (run in parallel,
// isolated from the others). Pushed is true iff the child pushed its 10x
// branch; Receipt is that push's receipt; Err is the failure reason when not.
type RepoAttempt struct {
	Pushed  bool
	Receipt integrator.Receipt
	Err     string
}

// RepoStatus is one repo's terminal status within a change-set.
type RepoStatus string

const (
	// RepoIntegrated: pushed and integrated (all dependencies integrated).
	RepoIntegrated RepoStatus = "integrated"
	// RepoPushedBlocked: the child pushed its branch, but a dependency failed
	// so it was not integrated. The branch is NOT auto-reverted — humans own
	// remediation (Constitution C15 / multi-repository.md).
	RepoPushedBlocked RepoStatus = "pushed-blocked"
	// RepoFailed: the child failed to push.
	RepoFailed RepoStatus = "failed"
)

// RepoReceipt is one repo's entry in the change-set receipt map.
type RepoReceipt struct {
	Alias   string
	Status  RepoStatus
	Receipt integrator.Receipt
	Error   string
}

// ChangeSetResult is the terminal, all-or-honest-partial outcome of a
// change-set saga. On any repo failure the saga ends PROVEN_BLOCKED with an
// exact per-repo receipt map and a human next_action — it never
// auto-reverts pushed shared branches.
type ChangeSetResult struct {
	InitiativeID    string
	Status          state.Status
	ResultCode      state.ResultCode
	ContractDigests []string
	EnvRevision     string
	Receipts        map[string]RepoReceipt
	NextAction      string
}

// FreezeContracts returns the sorted, frozen digest list plus a single
// combined digest over all declared cross-repo contracts. Freezing at saga
// start pins the interface snapshot every per-repo child integrates against.
func FreezeContracts(contracts []CrossRepoContract) ([]string, string) {
	digests := make([]string, 0, len(contracts))
	for _, c := range contracts {
		digests = append(digests, c.Name+"@"+c.Digest)
	}
	sort.Strings(digests)
	sum := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return digests, hex.EncodeToString(sum[:])
}

// topoOrder returns a deterministic topological ordering of repos by their
// DependsOn edges (Kahn's algorithm with sorted tie-breaks), or an error
// naming a dependency cycle / unknown dependency.
func topoOrder(repos []RepoNode) ([]string, error) {
	indeg := map[string]int{}
	deps := map[string][]string{}
	known := map[string]bool{}
	for _, r := range repos {
		known[r.Alias] = true
	}
	for _, r := range repos {
		if _, ok := indeg[r.Alias]; !ok {
			indeg[r.Alias] = 0
		}
		for _, d := range r.DependsOn {
			if !known[d] {
				return nil, fmt.Errorf("repo %q depends on unknown repo %q", r.Alias, d)
			}
			deps[d] = append(deps[d], r.Alias)
			indeg[r.Alias]++
		}
	}
	var ready []string
	for alias, d := range indeg {
		if d == 0 {
			ready = append(ready, alias)
		}
	}
	sort.Strings(ready)

	var order []string
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		order = append(order, next)
		children := deps[next]
		sort.Strings(children)
		for _, c := range children {
			indeg[c]--
			if indeg[c] == 0 {
				ready = append(ready, c)
			}
		}
		sort.Strings(ready)
	}
	if len(order) != len(repos) {
		return nil, fmt.Errorf("cross-repo dependency cycle detected among %d repos", len(repos))
	}
	return order, nil
}

// RunChangeSet resolves one change-set saga to its terminal result from the
// (already-executed, parallel, isolated) per-repo attempts. It freezes the
// contracts, integrates repos in dependency order, and applies
// all-or-honest-partial semantics: every pushed branch is recorded (never
// auto-reverted), and any failure ends the saga PROVEN_BLOCKED with an exact
// receipt map and a human next_action.
func RunChangeSet(plan ChangeSetPlan, attempts map[string]RepoAttempt) ChangeSetResult {
	digests, _ := FreezeContracts(plan.Contracts)
	res := ChangeSetResult{
		InitiativeID:    plan.InitiativeID,
		ContractDigests: digests,
		EnvRevision:     plan.EnvRevision,
		Receipts:        map[string]RepoReceipt{},
	}

	order, err := topoOrder(plan.Repos)
	if err != nil {
		// Cannot even order integration: fail closed, record every repo's raw
		// attempt so pushed branches are still surfaced for human cleanup.
		for _, r := range plan.Repos {
			a := attempts[r.Alias]
			rr := RepoReceipt{Alias: r.Alias, Receipt: a.Receipt}
			if a.Pushed {
				rr.Status = RepoPushedBlocked
			} else {
				rr.Status = RepoFailed
				rr.Error = a.Err
			}
			res.Receipts[r.Alias] = rr
		}
		res.Status = state.StatusFailed
		res.ResultCode = state.ResultProvenBlocked
		res.NextAction = "resolve cross-repo dependency graph error (" + err.Error() + "); no shared branch was auto-reverted"
		return res
	}

	depsOf := map[string][]string{}
	for _, r := range plan.Repos {
		depsOf[r.Alias] = r.DependsOn
	}

	integrated := map[string]bool{}
	var failedRepos []string
	for _, alias := range order {
		a := attempts[alias]
		depsOK := true
		for _, d := range depsOf[alias] {
			if !integrated[d] {
				depsOK = false
				break
			}
		}
		switch {
		case a.Pushed && depsOK:
			integrated[alias] = true
			res.Receipts[alias] = RepoReceipt{Alias: alias, Status: RepoIntegrated, Receipt: a.Receipt}
		case a.Pushed && !depsOK:
			// Pushed but a dependency failed: recorded, not integrated, NOT
			// reverted (humans own shared-branch remediation).
			res.Receipts[alias] = RepoReceipt{Alias: alias, Status: RepoPushedBlocked, Receipt: a.Receipt}
		default:
			failedRepos = append(failedRepos, alias)
			res.Receipts[alias] = RepoReceipt{Alias: alias, Status: RepoFailed, Error: a.Err}
		}
	}

	if len(failedRepos) == 0 && len(integrated) == len(plan.Repos) {
		res.Status = state.StatusSucceeded
		res.ResultCode = state.ResultTenXBranchHandoffReady
		res.NextAction = ""
		return res
	}

	res.Status = state.StatusFailed
	res.ResultCode = state.ResultProvenBlocked
	sort.Strings(failedRepos)
	pushed := pushedBranches(res.Receipts)
	res.NextAction = fmt.Sprintf(
		"change-set blocked by repo(s): %s. Pushed branches (%s) were NOT auto-reverted — a human must decide whether to complete, revert, or re-run per multi-repository.md.",
		strings.Join(failedRepos, ", "), strings.Join(pushed, ", "),
	)
	return res
}

// pushedBranches lists, sorted, the branches of every repo that pushed
// (integrated or pushed-blocked) — the set a human must reconcile.
func pushedBranches(receipts map[string]RepoReceipt) []string {
	var out []string
	for _, rr := range receipts {
		if rr.Status == RepoIntegrated || rr.Status == RepoPushedBlocked {
			b := rr.Receipt.Branch
			if b == "" {
				b = rr.Alias
			}
			out = append(out, b)
		}
	}
	sort.Strings(out)
	return out
}
