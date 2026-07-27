package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

const profileTimeout = 10 * time.Second

// runProfileCreate implements `foundry profile create` (docs/PLAN.md
// Task 21 / FND-02): reads a config JSON file, validates it against
// config/schemas/profile.schema.json (via profile.Store.Save), and inserts
// the profile.
//
// decision: Task 22 (FND-03, the policy compiler) is the component that
// derives a real policy_digest from a profile's resolved policy; that
// compiler does not exist yet, so this CLI seeds policy_digest as the
// sha256 of the profile's canonical config bytes — a placeholder, not a
// policy digest, but one that changes whenever config changes and is
// deterministic given the "no genuinely unspecified detail" no-gaps rule
// (docs/PLAN.md §A). Task 22 will replace this call site's digest source.
func runProfileCreate(args []string) error {
	fs := flag.NewFlagSet("profile create", flag.ContinueOnError)
	id := fs.String("id", "", "profile id (required)")
	name := fs.String("name", "", "profile name (required)")
	kind := fs.String("kind", "", "profile kind: personal|organization (required)")
	orgID := fs.String("org-id", "", "organization id (required when kind=organization)")
	configPath := fs.String("config", "", "path to a JSON file matching config/schemas/profile.schema.json (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *name == "" || *kind == "" || *configPath == "" {
		return fmt.Errorf("profile create: usage: foundry profile create -id=<id> -name=<name> -kind=personal|organization [-org-id=<id>] -config=<path>")
	}

	configBytes, err := os.ReadFile(*configPath)
	if err != nil {
		return fmt.Errorf("profile create: read config %s: %w", *configPath, err)
	}

	p := &profile.Profile{
		ID:           *id,
		Name:         *name,
		Kind:         profile.Kind(*kind),
		Config:       json.RawMessage(configBytes),
		PolicyDigest: placeholderPolicyDigest(configBytes),
	}
	if *orgID != "" {
		p.OrgID = orgID
	}

	raw, err := profile.OpenPGRawStore(pgDSNFromEnv())
	if err != nil {
		return fmt.Errorf("profile create: %w", err)
	}
	defer func() { _ = raw.Close() }()
	store := profile.NewStore(raw)

	ctx, cancel := context.WithTimeout(context.Background(), profileTimeout)
	defer cancel()

	if err := store.Save(ctx, p); err != nil {
		return fmt.Errorf("profile create: %w", err)
	}
	return printProfile(p)
}

// runProfileShow implements `foundry profile show <id>`.
func runProfileShow(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("profile show: usage: foundry profile show <id>")
	}

	raw, err := profile.OpenPGRawStore(pgDSNFromEnv())
	if err != nil {
		return fmt.Errorf("profile show: %w", err)
	}
	defer func() { _ = raw.Close() }()
	store := profile.NewStore(raw)

	ctx, cancel := context.WithTimeout(context.Background(), profileTimeout)
	defer cancel()

	p, err := store.Load(ctx, args[0])
	if err != nil {
		return fmt.Errorf("profile show: %w", err)
	}
	return printProfile(p)
}

// runProfileList implements `foundry profile list`.
func runProfileList(_ []string) error {
	raw, err := profile.OpenPGRawStore(pgDSNFromEnv())
	if err != nil {
		return fmt.Errorf("profile list: %w", err)
	}
	defer func() { _ = raw.Close() }()
	store := profile.NewStore(raw)

	ctx, cancel := context.WithTimeout(context.Background(), profileTimeout)
	defer cancel()

	profiles, err := store.List(ctx)
	if err != nil {
		return fmt.Errorf("profile list: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(profiles)
}

func printProfile(p *profile.Profile) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

func placeholderPolicyDigest(config []byte) string {
	sum := sha256.Sum256(config)
	return hex.EncodeToString(sum[:])
}
