package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// runPlanSubmit implements `foundry plan submit <file>`: parses and
// validates the plan file and prints the PlanSubmission artifact
// (docs/PLAN.md Task 8 Step 1). It performs no admission decision and no
// persistence — those happen at `plan approve`.
func runPlanSubmit(args []string) error {
	fs := flag.NewFlagSet("plan submit", flag.ContinueOnError)
	submitter := fs.String("submitter", os.Getenv("FOUNDRY_PRINCIPAL"), "submitting principal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("plan submit: usage: foundry plan submit <file>")
	}
	path := fs.Arg(0)

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("plan submit: read %s: %w", path, err)
	}
	doc, err := plan.ParseBytes(raw)
	if err != nil {
		return fmt.Errorf("plan submit: parse %s: %w", path, err)
	}

	submission := provenance.PlanSubmission{
		Digest:    doc.DigestHex(),
		Source:    path,
		Submitter: *submitter,
		At:        time.Now().UTC(),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(submission)
}
