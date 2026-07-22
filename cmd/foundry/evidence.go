package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
)

// evidenceRoot returns the evidence store root: $FOUNDRY_DATA_DIR/evidence,
// defaulting to ./data/evidence when FOUNDRY_DATA_DIR is unset.
func evidenceRoot() string {
	dataDir := os.Getenv("FOUNDRY_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	return filepath.Join(dataDir, "evidence")
}

// runEvidenceVerify implements `foundry evidence verify <id>` (docs/PLAN.md
// Task 11 Step 3): re-hashes every artifact and the manifest itself from
// bytes on disk.
func runEvidenceVerify(args []string) error {
	fs := flag.NewFlagSet("evidence verify", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("evidence verify: usage: foundry evidence verify <id>")
	}
	id := fs.Arg(0)

	store := evidence.NewFSStore(evidenceRoot())
	if err := store.Verify(id); err != nil {
		return fmt.Errorf("evidence verify: %w", err)
	}
	fmt.Printf("PASS: bundle %s verified\n", id)
	return nil
}

// runEvidenceShow implements `foundry evidence show <id>`: prints the
// stored manifest without re-verifying it.
func runEvidenceShow(args []string) error {
	fs := flag.NewFlagSet("evidence show", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("evidence show: usage: foundry evidence show <id>")
	}
	id := fs.Arg(0)

	store := evidence.NewFSStore(evidenceRoot())
	bundle, err := store.Get(id)
	if err != nil {
		return fmt.Errorf("evidence show: %w", err)
	}

	m := bundle.Manifest
	fmt.Printf("workflow_id: %s\n", m.WorkflowID)
	fmt.Printf("task_id:     %s\n", m.TaskID)
	fmt.Printf("created_at:  %s\n", m.CreatedAt)
	fmt.Printf("commands:    %d\n", len(m.Commands))
	for _, c := range m.Commands {
		fmt.Printf("  - %s (exit=%d, duration=%dms, stdout_sha256=%s)\n", c.Cmd, c.ExitCode, c.DurationMS, c.StdoutDigest)
	}
	fmt.Printf("artifacts:   %d\n", len(m.Artifacts))
	for _, a := range m.Artifacts {
		fmt.Printf("  - %s (sha256=%s, bytes=%d)\n", a.Path, a.SHA256, a.Bytes)
	}
	fmt.Printf("transitions: %d\n", len(m.Transitions))
	return nil
}
