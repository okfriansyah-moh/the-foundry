package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

const policyTimeout = 10 * time.Second

// runPolicyResolve implements `foundry policy resolve --profile X`
// (docs/PLAN.md Task 22 / FND-03): loads the named profile, folds it in as
// the profile layer atop the embedded platform defaults, and prints the
// effective policy plus every override's explanation.
//
// decision: Task 21's organizations table (internal/identity) carries no
// policy fields of its own, and no workflow-definition source exists yet
// (that lands with a later workflow-graph task), so this command passes
// empty org and workflow layers — every override in a real run today comes
// from the profile layer alone. This is the smallest reversible option
// (docs/PLAN.md §A no-gaps rule); org-policy rows or workflow-definition
// sources plug into the same Compile call when those schema surfaces land.
func runPolicyResolve(args []string) error {
	fs := flag.NewFlagSet("policy resolve", flag.ContinueOnError)
	profileID := fs.String("profile", "", "profile id to resolve (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profileID == "" {
		return fmt.Errorf("policy resolve: usage: foundry policy resolve --profile=<id>")
	}

	platform, err := compiler.PlatformDefaults()
	if err != nil {
		return fmt.Errorf("policy resolve: %w", err)
	}

	raw, err := profile.OpenPGRawStore(pgDSNFromEnv())
	if err != nil {
		return fmt.Errorf("policy resolve: %w", err)
	}
	defer func() { _ = raw.Close() }()
	store := profile.NewStore(raw)

	ctx, cancel := context.WithTimeout(context.Background(), policyTimeout)
	defer cancel()

	p, err := store.Load(ctx, *profileID)
	if err != nil {
		return fmt.Errorf("policy resolve: load profile %s: %w", *profileID, err)
	}

	profileLayer, err := compiler.ProfileLayerFromConfig(p.Config)
	if err != nil {
		return fmt.Errorf("policy resolve: %w", err)
	}

	resolved, err := compiler.Compile(platform, compiler.LayerPolicy{}, profileLayer, compiler.LayerPolicy{})
	if err != nil {
		return fmt.Errorf("policy resolve: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resolved.Effective); err != nil {
		return fmt.Errorf("policy resolve: %w", err)
	}
	fmt.Printf("digest: %s\n", resolved.Digest)
	fmt.Print(compiler.Explain(resolved))
	return nil
}
