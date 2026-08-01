package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// runPromotions implements `foundry promotions <subcommand>`.
// Task 52 (VEN-13): the only user-facing subcommand is `unfreeze`, which
// lifts a frozen improvement lease for a product (audited).
func runPromotions(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: foundry promotions <unfreeze>")
		return fmt.Errorf("promotions: subcommand required")
	}
	switch args[0] {
	case "unfreeze":
		return runPromotionsUnfreeze(args[1:])
	default:
		return fmt.Errorf("unknown promotions subcommand: %s", args[0])
	}
}

// runPromotionsUnfreeze implements `foundry promotions unfreeze --product <id>`.
// Task 127 (VEN-17): it genuinely DELETES the improvement_leases row for the
// product AND clears the durable improvement_freeze latch, then writes an
// audit_log entry — so a freeze set by the daemon is actually cleared and the
// action is actually audited, making this command's own doc comment true
// instead of false.
func runPromotionsUnfreeze(args []string) error {
	fs := flag.NewFlagSet("promotions unfreeze", flag.ContinueOnError)
	product := fs.String("product", "", "product ID to unfreeze")
	pgDSN := fs.String("pg-dsn", pgDSNFromEnv(), "Postgres DSN")
	actor := fs.String("actor", defaultActor(), "operator performing the unfreeze (audited)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *product == "" {
		return fmt.Errorf("promotions unfreeze: --product is required")
	}
	if *pgDSN == "" {
		return fmt.Errorf("promotions unfreeze: no Postgres DSN (set --pg-dsn or PG_DSN)")
	}

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("promotions unfreeze: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Clear the in-process latch too, so a single-process run reflects the
	// change immediately; the durable store below is the cross-process source
	// of truth.
	evolve.Unfreeze()

	freezeStore := evolve.NewFreezeStore(db)
	clearedFreeze, err := freezeStore.Unfreeze(ctx, *product)
	if err != nil {
		return fmt.Errorf("promotions unfreeze: clear freeze: %w", err)
	}

	res, err := db.ExecContext(ctx, `DELETE FROM improvement_leases WHERE product_id = $1`, *product)
	if err != nil {
		return fmt.Errorf("promotions unfreeze: delete lease: %w", err)
	}
	leaseRows, _ := res.RowsAffected()

	payload, _ := json.Marshal(map[string]any{
		"product":        *product,
		"lease_deleted":  leaseRows > 0,
		"freeze_cleared": clearedFreeze,
	})
	if err := provenance.AppendAuditRow(ctx, db, *actor, "promotions.unfreeze", *product, payload); err != nil {
		return fmt.Errorf("promotions unfreeze: write audit row: %w", err)
	}

	fmt.Printf("promotions unfreeze: product %q — lease deleted=%v, freeze cleared=%v (audited by %s)\n",
		*product, leaseRows > 0, clearedFreeze, *actor)
	return nil
}

func defaultActor() string {
	if v := os.Getenv("FOUNDRY_ACTOR"); v != "" {
		return v
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "operator"
}
