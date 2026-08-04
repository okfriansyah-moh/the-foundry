package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPackageCatalogValidateCommands(t *testing.T) {
	for _, kind := range []string{"agents", "skills"} {
		t.Run(kind, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := runPackageCatalog(kind, []string{"validate", "-root", "../.."}, &stdout); err != nil {
				t.Fatalf("runPackageCatalog: %v", err)
			}
			if !strings.Contains(stdout.String(), "catalog valid") {
				t.Fatalf("output = %q, want validation result", stdout.String())
			}
		})
	}
}

func TestPackageCatalogListIsSortedAndShowsEnablement(t *testing.T) {
	var stdout bytes.Buffer
	if err := runPackageCatalog("agents", []string{"list", "-root", "../.."}, &stdout); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "backend\tagent\ttrue") || strings.Index(output, "backend\t") > strings.Index(output, "verification\t") {
		t.Fatalf("agents list is missing enablement or not sorted:\n%s", output)
	}
	stdout.Reset()
	if err := runPackageCatalog("skills", []string{"list", "-root", "../.."}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "commercial-readiness\tdomain-skill\ttrue") {
		t.Fatalf("skills list missing enabled domain package:\n%s", stdout.String())
	}
}

func TestPackageCatalogRejectsUnknownSubcommand(t *testing.T) {
	if err := runPackageCatalog("agents", []string{"unknown"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown subcommand must fail")
	}
}

func TestPackageCatalogInstallAndDoctor(t *testing.T) {
	workspace := t.TempDir()
	for _, kind := range []string{"agents", "skills"} {
		var stdout bytes.Buffer
		args := []string{"install", "-root", "../..", "-workspace", workspace, "-provider", "claude-code"}
		if err := runPackageCatalog(kind, args, &stdout); err != nil {
			t.Fatalf("%s install: %v", kind, err)
		}
		stdout.Reset()
		args[0] = "doctor"
		if err := runPackageCatalog(kind, args, &stdout); err != nil {
			t.Fatalf("%s doctor: %v", kind, err)
		}
		if !strings.Contains(stdout.String(), "doctor claude-code") {
			t.Fatalf("%s doctor output = %q", kind, stdout.String())
		}
	}
}

func TestPackageCatalogRejectsUnsupportedProvider(t *testing.T) {
	err := runPackageCatalog("agents", []string{"install", "-root", "../..", "-workspace", t.TempDir(), "-provider", "openhands"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("error = %v, want unsupported provider", err)
	}
}

func TestSkillRollbackRequiresExplicitSkill(t *testing.T) {
	err := runSkillRollback([]string{"-root", t.TempDir()}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("error = %v, want explicit rollback usage", err)
	}
}
