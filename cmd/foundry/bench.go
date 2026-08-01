package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/okfriansyah-moh/the-foundry/internal/bench"
)

func runBench(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("bench: usage: foundry bench <baseline|report|foundry>")
	}
	switch args[0] {
	case "baseline":
		return runBenchBaseline(args[1:])
	case "report":
		return runBenchReport(args[1:])
	case "foundry":
		return runBenchFoundryArm(args[1:])
	default:
		return fmt.Errorf("bench: unknown subcommand %q", args[0])
	}
}

func runBenchBaseline(args []string) error {
	fs := flag.NewFlagSet("bench baseline", flag.ContinueOnError)
	dir := fs.String("dir", bench.DefaultBaselineDir, "baseline evidence directory")
	repo := fs.String("repo", ".", "git repository root to mine")
	targetsPath := fs.String("targets", bench.DefaultTargetsPath, "V1 acceptance targets config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	repoRoot, err := filepath.Abs(*repo)
	if err != nil {
		return fmt.Errorf("bench baseline: repo path: %w", err)
	}
	baselineDir, err := filepath.Abs(*dir)
	if err != nil {
		return fmt.Errorf("bench baseline: dir path: %w", err)
	}
	records, err := bench.CaptureBaseline(context.Background(), repoRoot, baselineDir)
	if err != nil {
		return fmt.Errorf("bench baseline: %w", err)
	}
	targets, err := bench.LoadTargets(*targetsPath)
	if err != nil {
		return fmt.Errorf("bench baseline: %w", err)
	}
	report := bench.Compare(records, nil)
	md := bench.RenderMarkdown(report, targets)
	reportPath := filepath.Join(baselineDir, "report.md")
	if err := bench.WriteReport(reportPath, md); err != nil {
		return fmt.Errorf("bench baseline: write report: %w", err)
	}
	fmt.Printf("bench baseline: recorded %d control-arm runs in %s\n", len(records), baselineDir)
	fmt.Printf("bench baseline: report written to %s\n", reportPath)
	for _, r := range records {
		fmt.Printf("  - %s (%s)\n", r.ID, r.WorkItemTitle)
	}
	return nil
}

func runBenchReport(args []string) error {
	fs := flag.NewFlagSet("bench report", flag.ContinueOnError)
	baselineDir := fs.String("baseline", bench.DefaultBaselineDir, "control-arm records directory")
	foundryDir := fs.String("foundry", "benchmarks/foundry", "foundry-arm records directory")
	targetsPath := fs.String("targets", bench.DefaultTargetsPath, "V1 acceptance targets config")
	out := fs.String("out", "", "write markdown report to path (default stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	controlStore := bench.NewFileStore(*baselineDir)
	foundryStore := bench.NewFileStore(*foundryDir)
	control, err := controlStore.LoadAll()
	if err != nil {
		return fmt.Errorf("bench report: load control: %w", err)
	}
	foundry, err := foundryStore.LoadAll()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("bench report: load foundry: %w", err)
	}
	targets, err := bench.LoadTargets(*targetsPath)
	if err != nil {
		return fmt.Errorf("bench report: %w", err)
	}
	report := bench.Compare(control, foundry)
	md := bench.RenderMarkdown(report, targets)
	if *out == "" {
		fmt.Print(md)
		return nil
	}
	return bench.WriteReport(*out, md)
}

// runBenchFoundryArm evaluates Foundry-arm records against the baseline
// (docs/PLAN.md Task 135). Missing Foundry-arm data yields an insufficient-data
// report rather than invented figures.
func runBenchFoundryArm(args []string) error {
	fs := flag.NewFlagSet("bench foundry", flag.ContinueOnError)
	baselineDir := fs.String("baseline", bench.DefaultBaselineDir, "control-arm records directory")
	foundryDir := fs.String("foundry", "benchmarks/foundry", "foundry-arm records directory")
	targetsPath := fs.String("targets", bench.DefaultTargetsPath, "V1 acceptance targets config")
	out := fs.String("out", "benchmarks/report-v1.md", "comparison report path")
	notes := fs.String("notes", "docs/notes/m5-acceleration-verdict.md", "published verdict path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	controlStore := bench.NewFileStore(*baselineDir)
	foundryStore := bench.NewFileStore(*foundryDir)
	control, err := controlStore.LoadAll()
	if err != nil {
		return fmt.Errorf("bench foundry: load control: %w", err)
	}
	foundry, err := foundryStore.LoadAll()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("bench foundry: load foundry: %w", err)
	}
	targets, err := bench.LoadTargets(*targetsPath)
	if err != nil {
		return fmt.Errorf("bench foundry: %w", err)
	}
	report := bench.Compare(control, foundry)
	md := bench.RenderMarkdown(report, targets)
	if err := bench.WriteReport(*out, md); err != nil {
		return err
	}
	if err := bench.WriteReport(*notes, md); err != nil {
		return err
	}
	fmt.Printf("bench foundry: wrote %s and %s (control=%d foundry=%d)\n", *out, *notes, len(control), len(foundry))
	return nil
}
