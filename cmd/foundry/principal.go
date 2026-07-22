package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/identity"
)

const principalTimeout = 10 * time.Second

// runPrincipalCreate implements `foundry principal create` (docs/PLAN.md
// Task 21 / FND-02): inserts one row into principals via internal/identity's
// Postgres-backed Store.
func runPrincipalCreate(args []string) error {
	fs := flag.NewFlagSet("principal create", flag.ContinueOnError)
	id := fs.String("id", "", "principal id (required)")
	kind := fs.String("kind", "", "principal kind: human|service (required)")
	display := fs.String("display", "", "display name (required)")
	idpSubject := fs.String("idp-subject", "", "IdP subject, if any")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *kind == "" || *display == "" {
		return fmt.Errorf("principal create: usage: foundry principal create -id=<id> -kind=human|service -display=<name> [-idp-subject=<sub>]")
	}

	p := &identity.Principal{
		ID:      *id,
		Kind:    identity.PrincipalKind(*kind),
		Display: *display,
	}
	if *idpSubject != "" {
		p.IDPSubject = idpSubject
	}

	store, err := identity.OpenPGStore(pgDSNFromEnv())
	if err != nil {
		return fmt.Errorf("principal create: %w", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), principalTimeout)
	defer cancel()

	if err := store.CreatePrincipal(ctx, p); err != nil {
		return fmt.Errorf("principal create: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}
