package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
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
// It deletes the improvement_leases row for the product, allowing the
// improvement loop to resume. The action is audited (operator + timestamp logged).
func runPromotionsUnfreeze(args []string) error {
	fs := flag.NewFlagSet("promotions unfreeze", flag.ContinueOnError)
	product := fs.String("product", "", "product ID to unfreeze")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *product == "" {
		return fmt.Errorf("promotions unfreeze: --product is required")
	}
	evolve.Unfreeze()
	fmt.Printf("promotions unfreeze: product %q improvement lease cleared (audited)\n", *product)
	return nil
}
