package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// sha256sumHex returns the SHA-256 hex digest of content.
func sha256sumHex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// runPlanSubmit implements `foundry plan submit <file>`: parses and
// validates the plan file and prints the PlanSubmission artifact
// (docs/PLAN.md Task 8 Step 1). It performs no admission decision and no
// persistence — those happen at `plan approve`.
//
// For org-profile submissions (Task 55 / TX-02), the --org flag enables
// --repo and --rev flags that auto-compute source_digests for org provenance
// validation. The computed OrgPlanSource is embedded in the submission output.
func runPlanSubmit(args []string) error {
	fs := flag.NewFlagSet("plan submit", flag.ContinueOnError)
	submitter := fs.String("submitter", os.Getenv("FOUNDRY_PRINCIPAL"), "submitting principal")
	apiAddr := fs.String("api-addr", os.Getenv("FOUNDRY_API_ADDR"), "foundryd API base URL; when set, the plan is submitted over the API instead of parsed locally")
	// Task 55 (TX-02): org provenance flags.
	orgMode := fs.Bool("org", false, "org-profile submission: enable source-digest computation")
	orgRepo := fs.String("repo", "", "org: source repository URL")
	orgRev := fs.String("rev", "", "org: source revision (git SHA)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("plan submit: usage: foundry plan submit [--org --repo <url> --rev <sha>] <file>")
	}
	path := fs.Arg(0)

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("plan submit: read %s: %w", path, err)
	}

	// docs/PLAN.md Task 36 dogfood: --api-addr routes over foundryd HTTP API.
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

	// Task 55: org-mode source digest computation.
	if *orgMode {
		if *orgRepo == "" || *orgRev == "" {
			return fmt.Errorf("plan submit --org: --repo and --rev are required")
		}
		srcDigest, err := computeOrgSourceDigests(path, raw, *orgRepo, *orgRev)
		if err != nil {
			return fmt.Errorf("plan submit --org: compute source digests: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Submission provenance.PlanSubmission
			OrgSource  interface{}
		}{submission, srcDigest})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(submission)
}

// computeOrgSourceDigests computes the OrgPlanSource for an org-mode submission.
// It produces a single SourceRef for the plan file itself (the source whose
// digest the validator will verify). Additional source files can be added by
// future org-workflow tooling.
func computeOrgSourceDigests(planPath string, content []byte, repo, rev string) (interface{}, error) {
	type srcRef struct {
		Path   string `json:"path"`
		Digest string `json:"digest"`
	}
	type orgSource struct {
		Repo          string   `json:"repo"`
		Revision      string   `json:"revision"`
		SourceDigests []srcRef `json:"source_digests"`
	}
	sum := sha256sumHex(content)
	return orgSource{
		Repo:     repo,
		Revision: rev,
		SourceDigests: []srcRef{
			{Path: planPath, Digest: sum},
		},
	}, nil
}
