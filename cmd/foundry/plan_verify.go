package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// runPlanVerify implements `foundry plan verify <plan-id>` (docs/PLAN.md
// Task 8 Step 4): recomputes the plan file's digest, loads the
// ApprovedPlan (which verifies its Ed25519 signature), and prints a
// granted⊆requested proof.
func runPlanVerify(args []string) error {
	fs := flag.NewFlagSet("plan verify", flag.ContinueOnError)
	file := fs.String("file", "", "path to the plan file to recompute the digest from")
	keyDir := fs.String("key-dir", "", "approver key directory (default ~/.foundry/keys)")
	pgDSN := fs.String("pg-dsn", os.Getenv("PG_DSN"), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("plan verify: usage: foundry plan verify <plan-id> --file <plan.md>")
	}
	if *file == "" {
		return fmt.Errorf("plan verify: --file is required")
	}
	planID := fs.Arg(0)

	dir := *keyDir
	if dir == "" {
		d, err := provenance.DefaultKeyDir()
		if err != nil {
			return fmt.Errorf("plan verify: %w", err)
		}
		dir = d
	}
	pub, err := provenance.LoadPublicKey(dir)
	if err != nil {
		return fmt.Errorf("plan verify: %w", err)
	}

	if *pgDSN == "" {
		return errors.New("plan verify: no --pg-dsn/PG_DSN set; cannot load ApprovedPlan")
	}
	raw, err := provenance.OpenPGRawStore(*pgDSN)
	if err != nil {
		return fmt.Errorf("plan verify: %w", err)
	}
	defer func() { _ = raw.Close() }()

	store := provenance.NewStore(raw, pub)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := provenance.VerifyPlanFile(ctx, store, planID, *file)
	if err != nil {
		return fmt.Errorf("plan verify: %w", err)
	}

	fmt.Printf("plan_id:         %s\n", result.PlanID)
	fmt.Printf("file digest:     %s\n", result.FileDigest)
	fmt.Printf("approved digest: %s\n", result.ApprovedDigest)
	fmt.Printf("digest match:    %v\n", result.DigestMatches)
	fmt.Printf("granted subset of requested: %v (granted=%d requested=%d)\n",
		result.GrantedSubset, len(result.Granted), len(result.Requested))

	if !result.DigestMatches {
		return fmt.Errorf("plan verify: file digest does not match approved digest for %s", planID)
	}
	if !result.GrantedSubset {
		return fmt.Errorf("plan verify: granted permissions are not a subset of requested for %s", planID)
	}
	return nil
}
