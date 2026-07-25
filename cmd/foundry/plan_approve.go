package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

const defaultApprovalTTL = 30 * 24 * time.Hour

// runPlanApprove implements `foundry plan approve <file>` (docs/PLAN.md
// Task 8 Step 3): classifies the plan, refuses TierH without an explicit
// human acknowledgement, signs the resulting ApprovedPlan, and inserts it.
func runPlanApprove(args []string) error {
	fs := flag.NewFlagSet("plan approve", flag.ContinueOnError)
	submitter := fs.String("submitter", os.Getenv("FOUNDRY_PRINCIPAL"), "submitting principal")
	creator := fs.String("creator", "", "creator principal (default: same as --submitter)")
	dataClass := fs.String("data-class", "internal", "data classification")
	ttl := fs.Duration("ttl", defaultApprovalTTL, "approval validity duration")
	keyDir := fs.String("key-dir", "", "approver key directory (default ~/.foundry/keys)")
	allowlistPath := fs.String("allowlist", "config/permissions-allowlist.yaml", "permissions allowlist file")
	pgDSN := fs.String("pg-dsn", os.Getenv("PG_DSN"), "Postgres DSN")
	forceHAck := fs.Bool("force-h-ack", false, "acknowledge and proceed despite a TierH (human-required) classification")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("plan approve: usage: foundry plan approve <file>")
	}
	path := fs.Arg(0)
	if *creator == "" {
		*creator = *submitter
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("plan approve: read %s: %w", path, err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		return fmt.Errorf("plan approve: parse %s: %w", path, err)
	}

	decision, err := admission.Classify(doc, admission.NoopPolicyView{})
	if err != nil {
		return fmt.Errorf("plan approve: admission classification rejected: %w", err)
	}
	if decision.Tier == admission.TierH && !*forceHAck {
		return fmt.Errorf("plan approve: tier H requires human acknowledgement (rerun with --force-h-ack)")
	}

	printDecision(decision)

	allow, err := provenance.LoadAllowList(*allowlistPath)
	if err != nil {
		return fmt.Errorf("plan approve: %w", err)
	}

	dir := *keyDir
	if dir == "" {
		d, err := provenance.DefaultKeyDir()
		if err != nil {
			return fmt.Errorf("plan approve: %w", err)
		}
		dir = d
	}
	kp, err := provenance.LoadKeyPair(dir)
	if err != nil {
		return fmt.Errorf("plan approve: %w", err)
	}

	now := time.Now().UTC()
	approved, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:              doc.ID,
		PlanDigest:          doc.DigestHex(),
		CreatorPrincipal:    *creator,
		SubmittingPrincipal: *submitter,
		ClassifierVersion:   decision.ClassifierVersion,
		Declared:            decision.Declared,
		Requested:           doc.RequestedPermissions,
		Scope:               scopeFromDoc(doc),
		RiskTier:            decision.Tier,
		BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: doc.BudgetUSD, WorkflowUSD: doc.BudgetUSD},
		DataClass:           *dataClass,
		Approvers:           []provenance.Approver{{Principal: *submitter, Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:          now,
		ExpiresAt:           now.Add(*ttl),
	}, allow)
	if err != nil {
		return fmt.Errorf("plan approve: %w", err)
	}

	if err := provenance.Sign(kp.Private, approved); err != nil {
		return fmt.Errorf("plan approve: %w", err)
	}

	if *pgDSN == "" {
		return errors.New("plan approve: no --pg-dsn/PG_DSN set; cannot persist ApprovedPlan")
	}
	raw2, err := provenance.OpenPGRawStore(*pgDSN)
	if err != nil {
		return fmt.Errorf("plan approve: %w", err)
	}
	defer raw2.Close()

	store := provenance.NewStore(raw2, kp.Public)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Insert(ctx, approved); err != nil {
		return fmt.Errorf("plan approve: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(approved)
}

func scopeFromDoc(doc *plan.Document) provenance.Scope {
	var repos, branches []string
	seenPath := map[string]struct{}{}
	var paths []string
	for _, r := range doc.Repos {
		repos = append(repos, r.URL)
		branches = append(branches, r.Branch)
	}
	for _, t := range doc.Tasks {
		for _, f := range t.Files {
			if _, ok := seenPath[f]; ok {
				continue
			}
			seenPath[f] = struct{}{}
			paths = append(paths, f)
		}
	}
	return provenance.Scope{Repositories: repos, Paths: paths, Branches: branches}
}

func printDecision(d admission.Decision) {
	fmt.Printf("admission: tier=%s classifier=%s explanation=%q\n", d.Tier, d.ClassifierVersion, d.Explanation)
}
