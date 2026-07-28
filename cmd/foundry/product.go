package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/product"
)

func runProductNew(args []string) error {
	fs := flag.NewFlagSet("product new", flag.ContinueOnError)
	fromTemplate := fs.Bool("from-template", false, "instantiate from templates/product")
	name := fs.String("name", "", "product name")
	outDir := fs.String("out", ".", "destination directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*fromTemplate {
		return errors.New("product new: only --from-template is supported")
	}
	if *name == "" {
		return errors.New("product new: -name is required")
	}
	path, err := product.Instantiate(product.InstantiateOptions{
		Name:        *name,
		Destination: *outDir,
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stdout, path)
	return nil
}
