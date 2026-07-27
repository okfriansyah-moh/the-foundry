package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// runPlanRevoke implements `foundry plan revoke <id> --reason <reason>`
// (docs/PLAN.md Task 24 / Constitution C7): marks the ApprovedPlan
// revoked, re-signs it (Store.Revoke), and appends an audit_log row
// (Task 20's table, written via the minimal AppendAuditRow helper this
// task adds — see internal/provenance/audit.go's doc comment for why a
// full audit package was not built here). Revocation takes effect
// immediately: the very next Store.Load — including the kernel's
// RecheckApproval activity — sees it, with no caching window.
func runPlanRevoke(args []string) error {
	fs := flag.NewFlagSet("plan revoke", flag.ContinueOnError)
	reason := fs.String("reason", "", "reason for revocation (required)")
	revokedBy := fs.String("revoked-by", os.Getenv("FOUNDRY_PRINCIPAL"), "principal performing the revocation")
	keyDir := fs.String("key-dir", "", "approver key directory (default ~/.foundry/keys)")
	pgDSN := fs.String("pg-dsn", os.Getenv("PG_DSN"), "Postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("plan revoke: usage: foundry plan revoke <plan-id> --reason <reason>")
	}
	planID := fs.Arg(0)
	if *reason == "" {
		return fmt.Errorf("plan revoke: --reason is required")
	}
	if *revokedBy == "" {
		return fmt.Errorf("plan revoke: --revoked-by (or FOUNDRY_PRINCIPAL) is required")
	}

	dir := *keyDir
	if dir == "" {
		d, err := provenance.DefaultKeyDir()
		if err != nil {
			return fmt.Errorf("plan revoke: %w", err)
		}
		dir = d
	}
	kp, err := provenance.LoadKeyPair(dir)
	if err != nil {
		return fmt.Errorf("plan revoke: %w", err)
	}

	if *pgDSN == "" {
		return errors.New("plan revoke: no --pg-dsn/PG_DSN set; cannot persist revocation")
	}
	raw, err := provenance.OpenPGRawStore(*pgDSN)
	if err != nil {
		return fmt.Errorf("plan revoke: %w", err)
	}
	defer raw.Close()

	store := provenance.NewStore(raw, kp.Public)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	revoked, err := store.Revoke(ctx, planID, kp.Private, *revokedBy, *reason)
	if err != nil {
		return fmt.Errorf("plan revoke: %w", err)
	}

	payload, err := json.Marshal(struct {
		PlanID    string `json:"plan_id"`
		RevokedBy string `json:"revoked_by"`
		Reason    string `json:"reason"`
	}{PlanID: planID, RevokedBy: *revokedBy, Reason: *reason})
	if err != nil {
		return fmt.Errorf("plan revoke: marshal audit payload: %w", err)
	}
	if err := provenance.AppendAuditRow(ctx, raw.DB(), *revokedBy, "plan.revoke", planID, payload); err != nil {
		return fmt.Errorf("plan revoke: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(revoked)
}
