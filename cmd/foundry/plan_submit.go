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
	apiAddr := fs.String("api-addr", os.Getenv("FOUNDRY_API_ADDR"), "foundryd API base URL; when set, the plan is submitted over the API instead of parsed locally")
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

	// docs/PLAN.md Task 36 dogfood: --api-addr (or $FOUNDRY_API_ADDR)
	// routes this command over foundryd's HTTP API (POST /v1/plans)
	// instead of parsing the plan locally. Opt-in and additive — every
	// existing direct caller (test/skp_e2e.sh, test/skp_resume_test.sh,
	// test/provenance_e2e.sh, none of which set --api-addr) keeps its
	// current behavior unchanged. Submitter there is always the API's
	// session principal, not this --submitter flag — see
	// internal/api.handleSubmitPlan's own doc comment.
	if *apiAddr != "" {
		return runPlanSubmitViaAPI(*apiAddr, raw)
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
