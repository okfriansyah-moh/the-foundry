package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
)

// checkCapabilityStaleness (docs/PLAN.md Task 84 / PRV-01 staleness lint)
// loads every executor-capability registry reachable from roots and reports,
// by provider name, any record whose last_verified_at is more than
// capability.StalenessLimit (180d) old. A root may name the YAML file
// directly or a directory to search for "executor-capabilities.yaml".
//
// This is a genuine wall-clock check: a registry that was fresh at merge
// time will legitimately start failing 180 days later, which is the point —
// it forces re-verification of provider capabilities against the real CLI.
func checkCapabilityStaleness(roots []string) ([]string, error) {
	files, err := capabilityFiles(roots)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var violations []string
	for _, f := range files {
		reg, err := capability.Load(f)
		if err != nil {
			return nil, fmt.Errorf("capability staleness: %w", err)
		}
		for _, provider := range reg.Stale(now) {
			violations = append(violations, fmt.Sprintf(
				"%s: capability record for %q is stale (last_verified_at older than %d days) — re-verify against the provider and bump last_verified_at",
				f, provider, int(capability.StalenessLimit.Hours()/24)))
		}
	}
	return violations, nil
}

// capabilityFiles resolves roots into concrete registry file paths.
func capabilityFiles(roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, root)
			continue
		}
		err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				if skipDirNames[fi.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if fi.Name() == "executor-capabilities.yaml" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}
