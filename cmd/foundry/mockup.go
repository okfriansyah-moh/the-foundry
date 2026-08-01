package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
	"github.com/okfriansyah-moh/the-foundry/internal/spec/mockup"
)

func runMockup(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: foundry mockup extract --input <path|url> [--out <spec.json|plan.md>]")
	}
	switch args[0] {
	case "extract":
		return runMockupExtract(args[1:])
	default:
		return fmt.Errorf("unknown mockup subcommand: %s", args[0])
	}
}

func runMockupExtract(args []string) error {
	fs := flag.NewFlagSet("mockup extract", flag.ContinueOnError)
	input := fs.String("input", "", "mockup input path (required)")
	out := fs.String("out", "", "output path (.json spec or .md plan)")
	cassetteDir := fs.String("cassette-dir", "test/cassettes/mockup", "vision replay cassette directory")
	defaultsPath := fs.String("defaults", "config/spec-defaults.yaml", "spec defaults for PostPass")
	mappingPath := fs.String("effect-mapping", "config/effect-mapping.yaml", "effect mapping for plan generation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return fmt.Errorf("mockup extract: --input is required")
	}

	raw, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("mockup extract: read %s: %w", *input, err)
	}
	format, err := mockup.Detect(raw, filepath.Ext(*input))
	if err != nil {
		return fmt.Errorf("mockup extract: %w", err)
	}

	artifact := mockup.Artifact{
		Name:      filepath.Base(*input),
		MediaType: mockup.MediaTypeForFormat(format),
		Path:      *input,
	}

	ctx := context.Background()
	router := mockup.NewRouter(mockup.RouterConfig{CassetteDir: *cassetteDir})
	extraction, err := router.Extract(ctx, artifact, raw)
	if err != nil {
		return fmt.Errorf("mockup extract: %w", err)
	}

	defaults, err := spec.LoadDefaults(*defaultsPath)
	if err != nil {
		return fmt.Errorf("mockup extract: %w", err)
	}
	specOut := spec.PostPass(extraction.SeedRequirements, defaults)

	outPath := *out
	if outPath == "" {
		outPath = "spec.json"
	}

	if strings.HasSuffix(strings.ToLower(outPath), ".md") {
		mapping, err := loadEffectMappingCLI(*mappingPath)
		if err != nil {
			return fmt.Errorf("mockup extract: %w", err)
		}
		planID := "mockup-" + time.Now().UTC().Format("20060102-150405")
		rawPlan, err := spec.PlanFromSpecification(planID, "Mockup-derived plan", specOut, mapping, mockupMissionContext())
		if err != nil {
			return fmt.Errorf("mockup extract: %w", err)
		}
		if err := os.WriteFile(outPath, rawPlan, 0o644); err != nil {
			return fmt.Errorf("mockup extract: write %s: %w", outPath, err)
		}
		if _, err := fmt.Fprintf(os.Stdout, "wrote plan to %s (%d requirements)\n", outPath, len(specOut.Requirements)); err != nil {
			return err
		}
		return nil
	}

	encoded, err := json.MarshalIndent(specOut, "", "  ")
	if err != nil {
		return fmt.Errorf("mockup extract: marshal spec: %w", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o644); err != nil {
		return fmt.Errorf("mockup extract: write %s: %w", outPath, err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "wrote spec to %s (%d requirements)\n", outPath, len(specOut.Requirements)); err != nil {
		return err
	}
	return nil
}

func loadEffectMappingCLI(path string) (spec.EffectMapping, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return spec.EffectMapping{}, fmt.Errorf("read effect mapping %s: %w", path, err)
	}
	var mapping spec.EffectMapping
	if err := yaml.Unmarshal(raw, &mapping); err != nil {
		return spec.EffectMapping{}, fmt.Errorf("decode effect mapping %s: %w", path, err)
	}
	return mapping, nil
}

func mockupMissionContext() spec.MissionContext {
	return spec.MissionContext{
		RepoAlias:       "product",
		RepoURL:         "https://github.com/example/mission-repo",
		RepoBranch:      "main",
		BudgetUSD:       1000,
		RepoWriteTarget: "product/**",
	}
}
