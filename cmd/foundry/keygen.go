package main

import (
	"flag"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// runKeygen implements `foundry keygen`: generates an Ed25519 approver key
// pair and writes it to ~/.foundry/keys (or --dir), 0600 (docs/PLAN.md
// Task 8 Step 2).
func runKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	dir := fs.String("dir", "", "key directory (default ~/.foundry/keys)")
	force := fs.Bool("force", false, "overwrite an existing key pair")
	if err := fs.Parse(args); err != nil {
		return err
	}

	keyDir := *dir
	if keyDir == "" {
		d, err := provenance.DefaultKeyDir()
		if err != nil {
			return fmt.Errorf("keygen: %w", err)
		}
		keyDir = d
	}

	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	if err := provenance.WriteKeyPair(keyDir, kp, *force); err != nil {
		return fmt.Errorf("keygen: %w", err)
	}

	fmt.Printf("wrote approver key pair to %s (approver.pub, approver.key)\n", keyDir)
	return nil
}
